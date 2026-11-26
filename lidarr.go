package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrAlreadyAdded = errors.New("album already added")

// LidarrError is a non-2xx answer from Lidarr; Message is Lidarr's own text.
type LidarrError struct {
	Status  int
	Message string
}

func (e *LidarrError) Error() string { return fmt.Sprintf("lidarr %d: %s", e.Status, e.Message) }

type Lidarr struct {
	base, key string
	hc        *http.Client
}

// noRedirect stops a client at a redirect: Go would send the key header on to wherever it points.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func NewLidarr(base, key string) *Lidarr {
	return &Lidarr{base: strings.TrimRight(base, "/"), key: key, hc: &http.Client{CheckRedirect: noRedirect}}
}

type Album struct {
	ID                     int
	MBID                   string
	Title                  string
	Type                   string
	Secondary              []string
	Year                   int
	Cover                  string
	ArtistMBID, ArtistName string
	ArtistID               int
	Monitored              bool
	Releases               int
	Percent                float64
	ArtistMonitored        bool
	ArtistQualityProfileID int
}

type albumResource struct {
	ID             int       `json:"id"`
	ForeignAlbumID string    `json:"foreignAlbumId"`
	Title          string    `json:"title"`
	AlbumType      string    `json:"albumType"`
	SecondaryTypes []string  `json:"secondaryTypes"`
	ReleaseDate    time.Time `json:"releaseDate"`
	RemoteCover    string    `json:"remoteCover"`
	Monitored      bool      `json:"monitored"`
	Releases       []struct {
		ID int `json:"id"`
	} `json:"releases"`
	Statistics struct {
		PercentOfTracks float64 `json:"percentOfTracks"`
	} `json:"statistics"`
	Artist struct {
		ID               int    `json:"id"`
		ForeignArtistID  string `json:"foreignArtistId"`
		ArtistName       string `json:"artistName"`
		Monitored        bool   `json:"monitored"`
		QualityProfileID int    `json:"qualityProfileId"`
	} `json:"artist"`
}

func (r albumResource) album() Album {
	a := Album{ID: r.ID, MBID: r.ForeignAlbumID, Title: r.Title, Type: r.AlbumType, Secondary: r.SecondaryTypes,
		Cover: safeCover(r.RemoteCover), ArtistMBID: r.Artist.ForeignArtistID, ArtistName: r.Artist.ArtistName,
		ArtistID: r.Artist.ID, Monitored: r.Monitored, Releases: len(r.Releases), Percent: r.Statistics.PercentOfTracks,
		ArtistMonitored: r.Artist.Monitored, ArtistQualityProfileID: r.Artist.QualityProfileID}
	if !r.ReleaseDate.IsZero() {
		a.Year = r.ReleaseDate.Year()
	}
	if a.Secondary == nil {
		a.Secondary = []string{}
	}
	return a
}

// safeCover keeps only https covers from the hosts allowed by the page's CSP.
func safeCover(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return ""
	}
	switch u.Host {
	case "images.lidarr.audio", "imagecache.lidarr.audio": // not coverartarchive.org: it redirects to archive.org hosts the CSP blocks
		return raw
	}
	return ""
}

func (l *Lidarr) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	u := l.base + path
	if q != nil {
		u += "?" + q.Encode()
	}
	if _, ok := ctx.Deadline(); !ok { // callers that need longer (adding an artist) set their own deadline
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", l.key)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := l.hc.Do(req)
	if err != nil {
		return fmt.Errorf("lidarr injoignable: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return &LidarrError{Status: resp.StatusCode, Message: strings.ReplaceAll(lidarrMessage(data), l.key, "•••")}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

// lidarrMessage extracts the first human message from a validation array or an error object.
func lidarrMessage(data []byte) string {
	var list []struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(data, &list) == nil && len(list) > 0 && list[0].ErrorMessage != "" {
		return list[0].ErrorMessage
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return strings.TrimSpace(string(data[:min(len(data), 200)]))
}

func (l *Lidarr) Lookup(ctx context.Context, term string) ([]Album, error) {
	var res []albumResource
	if err := l.do(ctx, "GET", "/api/v1/album/lookup", url.Values{"term": {term}}, nil, &res); err != nil {
		return nil, err
	}
	out := make([]Album, 0, len(res))
	for _, r := range res {
		out = append(out, r.album())
	}
	return out, nil
}

type Artist struct {
	MBID           string `json:"mbid"`
	Name           string `json:"name"`
	Disambiguation string `json:"disambiguation,omitempty"`
	Image          string `json:"image,omitempty"`
}

func (l *Lidarr) ArtistLookup(ctx context.Context, term string) ([]Artist, error) {
	var res []struct {
		ForeignArtistID, ArtistName, Disambiguation, RemotePoster string
		Images                                                    []struct{ CoverType, RemoteURL string }
	}
	if err := l.do(ctx, "GET", "/api/v1/artist/lookup", url.Values{"term": {term}}, nil, &res); err != nil {
		return nil, err
	}
	out := make([]Artist, 0, len(res))
	for _, r := range res {
		img := r.RemotePoster
		for _, i := range r.Images {
			if img == "" && i.CoverType == "poster" {
				img = i.RemoteURL
			}
		}
		out = append(out, Artist{MBID: r.ForeignArtistID, Name: r.ArtistName, Disambiguation: r.Disambiguation, Image: safeCover(img)})
	}
	return out, nil
}

func (l *Lidarr) AlbumByMBID(ctx context.Context, mbid string) (*Album, error) {
	var res []albumResource
	if err := l.do(ctx, "GET", "/api/v1/album", url.Values{"foreignAlbumId": {mbid}}, nil, &res); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, nil
	}
	a := res[0].album()
	return &a, nil
}

// AddAlbum adds one album. albumsToMonitor is what keeps Lidarr from unmonitoring the
// requested album when it applies monitor "none" after the first scan of a new artist.
func (l *Lidarr) AddAlbum(ctx context.Context, a Album, qualityID, metadataID int, root string) (int, error) {
	payload := map[string]any{
		"foreignAlbumId": a.MBID,
		"monitored":      true,
		"anyReleaseOk":   true,
		"addOptions":     map[string]any{"searchForNewAlbum": false},
		"artist": map[string]any{
			"foreignArtistId":   a.ArtistMBID,
			"artistName":        a.ArtistName,
			"qualityProfileId":  qualityID,
			"metadataProfileId": metadataID,
			"rootFolderPath":    root,
			"monitored":         true,
			"monitorNewItems":   "none",
			"tags":              []int{},
			"addOptions": map[string]any{
				"monitor":                "none",
				"albumsToMonitor":        []string{a.MBID},
				"searchForMissingAlbums": false,
			},
		},
	}
	var res struct {
		ID int `json:"id"`
	}
	err := l.do(ctx, "POST", "/api/v1/album", nil, payload, &res)
	var le *LidarrError
	if errors.As(err, &le) && le.Status == 400 && strings.Contains(le.Message, "already been added") {
		return 0, ErrAlreadyAdded
	}
	return res.ID, err
}

func (l *Lidarr) MonitorAlbum(ctx context.Context, id int) error {
	return l.do(ctx, "PUT", "/api/v1/album/monitor", nil, map[string]any{"albumIds": []int{id}, "monitored": true}, nil)
}

// UnmonitorAlbum stops Lidarr looking for an album that reached Plex another way.
func (l *Lidarr) UnmonitorAlbum(ctx context.Context, id int) error {
	return l.do(ctx, "PUT", "/api/v1/album/monitor", nil, map[string]any{"albumIds": []int{id}, "monitored": false}, nil)
}

// ArtistKnown says whether Lidarr already has this artist.
func (l *Lidarr) ArtistKnown(ctx context.Context, artistMBID string) (bool, error) {
	var res []struct {
		ForeignArtistID string `json:"foreignArtistId"`
	}
	if err := l.do(ctx, "GET", "/api/v1/artist", url.Values{"mbId": {artistMBID}}, nil, &res); err != nil {
		return false, err
	}
	for _, a := range res { // also correct if this Lidarr ignores mbId and lists every artist
		if a.ForeignArtistID == artistMBID {
			return true, nil
		}
	}
	return false, nil
}

func (l *Lidarr) MonitorArtist(ctx context.Context, id int) error {
	return l.do(ctx, "PUT", "/api/v1/artist/editor", nil, map[string]any{"artistIds": []int{id}, "monitored": true}, nil)
}

// Releases runs Lidarr's interactive search for one album: the same one query per indexer as
// an AlbumSearch, but Musaarr sees every source and why Lidarr refused it.
func (l *Lidarr) Releases(ctx context.Context, albumID int) ([]Release, error) {
	var rels []Release
	err := l.do(ctx, "GET", "/api/v1/release", url.Values{"albumId": {strconv.Itoa(albumID)}}, nil, &rels)
	return rels, err
}

// Grab sends one source found by Releases to the download client. albumId and artistId tell
// Lidarr which album it is when it could not read the name itself.
func (l *Lidarr) Grab(ctx context.Context, r Release, albumID, artistID int) error {
	return l.do(ctx, "POST", "/api/v1/release", nil, struct {
		GUID      string `json:"guid"`
		IndexerID int    `json:"indexerId"`
		AlbumID   int    `json:"albumId"`
		ArtistID  int    `json:"artistId"`
	}{r.GUID, r.IndexerID, albumID, artistID}, nil)
}

type Command struct {
	Status  string    `json:"status"`
	Message string    `json:"message"`
	Ended   time.Time `json:"ended"`
	Gone    bool      `json:"gone,omitempty"`
}

// Command reads a command; a 404 (Lidarr restarted and forgot it) is reported as Gone.
func (l *Lidarr) Command(ctx context.Context, id int) (Command, error) {
	var c Command
	err := l.do(ctx, "GET", "/api/v1/command/"+strconv.Itoa(id), nil, nil, &c)
	var le *LidarrError
	if errors.As(err, &le) && le.Status == 404 {
		return Command{Gone: true}, nil
	}
	return c, err
}

type QueueItem struct {
	AlbumID    int     `json:"albumId"`
	Size       float64 `json:"size"`
	Sizeleft   float64 `json:"sizeleft"`
	Status     string  `json:"status"`
	State      string  `json:"trackedDownloadState"`
	Timeleft   string  `json:"timeleft"`
	DownloadID string  `json:"downloadId"`
}

// Queue reads the whole queue: a download Lidarr could not map to an album has no albumId.
func (l *Lidarr) Queue(ctx context.Context) ([]QueueItem, error) {
	var items []QueueItem
	err := l.do(ctx, "GET", "/api/v1/queue/details", nil, nil, &items)
	return items, err
}

// GrabbedDownloadID returns the download id of the album's latest grab, "" if Lidarr has none yet.
func (l *Lidarr) GrabbedDownloadID(ctx context.Context, albumID int) (string, error) {
	var page struct {
		Records []struct {
			EventType  string `json:"eventType"`
			DownloadID string `json:"downloadId"`
		} `json:"records"`
	}
	q := url.Values{"albumId": {strconv.Itoa(albumID)}, "eventType": {"1"}, "sortKey": {"date"}, "sortDirection": {"descending"}, "pageSize": {"10"}}
	if err := l.do(ctx, "GET", "/api/v1/history", q, nil, &page); err != nil {
		return "", err
	}
	for _, h := range page.Records {
		if h.EventType == "grabbed" && h.DownloadID != "" {
			return h.DownloadID, nil
		}
	}
	return "", nil
}

// ImportItem is one file of a download as Lidarr's manual import proposes to map it.
type ImportItem struct {
	Path  string `json:"path"`
	Album struct {
		ID int `json:"id"`
	} `json:"album"`
	AlbumReleaseID int `json:"albumReleaseId"`
	Tracks         []struct {
		ID int `json:"id"`
	} `json:"tracks"`
	Quality        json.RawMessage   `json:"quality"` // sent back as Lidarr gave it
	IndexerFlags   int               `json:"indexerFlags"`
	Rejections     []json.RawMessage `json:"rejections"`
	AdditionalFile bool              `json:"additionalFile"`
}

func (l *Lidarr) ImportItems(ctx context.Context, downloadID string, artistID int) ([]ImportItem, error) {
	var items []ImportItem
	q := url.Values{"downloadId": {downloadID}, "artistId": {strconv.Itoa(artistID)}, "filterExistingFiles": {"true"}, "replaceExistingFiles": {"false"}}
	err := l.do(ctx, "GET", "/api/v1/manualimport", q, nil, &items)
	return items, err
}

// ManualImport imports the files into the album, as Lidarr's own Manual Import screen does.
func (l *Lidarr) ManualImport(ctx context.Context, items []ImportItem, artistID, albumID int, downloadID string) (int, error) {
	type file struct {
		Path                    string          `json:"path"`
		ArtistID                int             `json:"artistId"`
		AlbumID                 int             `json:"albumId"`
		AlbumReleaseID          int             `json:"albumReleaseId"`
		TrackIDs                []int           `json:"trackIds"`
		Quality                 json.RawMessage `json:"quality"`
		IndexerFlags            int             `json:"indexerFlags"`
		DownloadID              string          `json:"downloadId"`
		DisableReleaseSwitching bool            `json:"disableReleaseSwitching"`
	}
	files := make([]file, 0, len(items))
	for _, it := range items {
		f := file{Path: it.Path, ArtistID: artistID, AlbumID: albumID, AlbumReleaseID: it.AlbumReleaseID,
			Quality: it.Quality, IndexerFlags: it.IndexerFlags, DownloadID: downloadID}
		for _, t := range it.Tracks {
			f.TrackIDs = append(f.TrackIDs, t.ID)
		}
		files = append(files, f)
	}
	var res struct {
		ID int `json:"id"`
	}
	err := l.do(ctx, "POST", "/api/v1/command", nil, struct {
		Name                 string `json:"name"`
		ImportMode           string `json:"importMode"`
		ReplaceExistingFiles bool   `json:"replaceExistingFiles"`
		Files                []file `json:"files"`
	}{"ManualImport", "auto", false, files}, &res)
	return res.ID, err
}

type Profiles struct {
	QualityID, MetadataID int
	QualityNames          map[int]string
	Allowed               map[int]bool // quality ids the quality profile accepts
}

// profileItem is a quality or a group of qualities; Lidarr only reads the group's allowed flag.
type profileItem struct {
	Quality struct {
		ID int `json:"id"`
	} `json:"quality"`
	Items   []profileItem `json:"items"`
	Allowed bool          `json:"allowed"`
}

func allowedQualities(items []profileItem, out map[int]bool) {
	for _, it := range items {
		if !it.Allowed {
			continue
		}
		if len(it.Items) == 0 {
			out[it.Quality.ID] = true
			continue
		}
		for _, q := range it.Items {
			q.Allowed = true
			allowedQualities([]profileItem{q}, out)
		}
	}
}

// Profiles resolves the configured profile names and checks the root folder exists.
func (l *Lidarr) Profiles(ctx context.Context, quality, metadata, root string) (Profiles, error) {
	type named struct {
		ID    int           `json:"id"`
		Name  string        `json:"name"`
		Path  string        `json:"path"`
		Items []profileItem `json:"items"`
	}
	var qs, ms, rs []named
	for path, dst := range map[string]*[]named{"/api/v1/qualityprofile": &qs, "/api/v1/metadataprofile": &ms, "/api/v1/rootfolder": &rs} {
		if err := l.do(ctx, "GET", path, nil, nil, dst); err != nil {
			return Profiles{}, err
		}
	}
	p := Profiles{QualityNames: map[int]string{}, Allowed: map[int]bool{}}
	for _, q := range qs {
		p.QualityNames[q.ID] = q.Name
		if q.Name == quality {
			p.QualityID = q.ID
			allowedQualities(q.Items, p.Allowed)
		}
	}
	for _, m := range ms {
		if m.Name == metadata {
			p.MetadataID = m.ID
		}
	}
	rootOK := false
	for _, r := range rs {
		rootOK = rootOK || strings.TrimRight(r.Path, "/") == strings.TrimRight(root, "/")
	}
	switch {
	case p.QualityID == 0:
		return p, fmt.Errorf("quality profile %q not found in Lidarr", quality)
	case p.MetadataID == 0:
		return p, fmt.Errorf("metadata profile %q not found in Lidarr", metadata)
	case !rootOK:
		return p, fmt.Errorf("root folder %q not found in Lidarr", root)
	}
	return p, nil
}
