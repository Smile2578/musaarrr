package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Metadata reads Lidarr's metadata server directly: Lidarr's own API cannot list
// the albums of an artist it does not have yet.
type Metadata struct {
	base string
	hc   *http.Client
}

func NewMetadata(base string) *Metadata {
	return &Metadata{base: strings.TrimRight(base, "/"), hc: &http.Client{Timeout: 30 * time.Second}}
}

func (m *Metadata) artist(ctx context.Context, mbid string, out any) error {
	return m.get(ctx, "/artist/"+url.PathEscape(mbid), out)
}

func (m *Metadata) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", m.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := m.hc.Do(req)
	if err != nil {
		return fmt.Errorf("metadata server unreachable: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return &LidarrError{Status: resp.StatusCode, Message: "serveur de métadonnées"}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

// Names returns the artist's MusicBrainz name, then its aliases ("Ye", "Kanye West", "Yeezy"...).
func (m *Metadata) Names(ctx context.Context, mbid string) ([]string, error) {
	var res struct {
		ArtistName    string
		ArtistAliases []string
	}
	if err := m.artist(ctx, mbid, &res); err != nil {
		return nil, err
	}
	var names []string
	for _, n := range append([]string{res.ArtistName}, res.ArtistAliases...) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return names, nil
}

// ArtistAlbums returns the artist's name and official releases, studio albums first.
func (m *Metadata) ArtistAlbums(ctx context.Context, mbid string) (string, []Album, error) {
	var res struct {
		ArtistName string
		Albums     []struct {
			ID, Title, Type, ReleaseDate    string
			SecondaryTypes, ReleaseStatuses []string
		}
	}
	if err := m.artist(ctx, mbid, &res); err != nil {
		return "", nil, err
	}
	var out []Album
	for _, r := range res.Albums {
		if !slices.Contains(r.ReleaseStatuses, "Official") { // bootlegs and promos only
			continue
		}
		a := Album{MBID: r.ID, Title: r.Title, Type: r.Type, Secondary: r.SecondaryTypes, ArtistMBID: mbid, ArtistName: res.ArtistName}
		if a.Secondary == nil {
			a.Secondary = []string{}
		}
		if len(r.ReleaseDate) >= 4 {
			a.Year, _ = strconv.Atoi(r.ReleaseDate[:4])
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return res.ArtistName, out, nil
}

// Track is one track of an edition, or of the version the user owns in Plex.
type Track struct {
	Disc  int    `json:"disc"`
	N     int    `json:"n"`
	Title string `json:"title"`
	Ms    int    `json:"ms"`
}

// Edition is one tracklist of an album: the pressings that share it (same titles, same order)
// count as one, so 20 releases read as the 3 or 4 versions that differ.
type Edition struct {
	Key            string     `json:"key"`  // MBID of its earliest release
	Name           string     `json:"name"` // « JP · 2006 · SHM-CD · 22 tracks », in English: see EditionParts
	Title          string     `json:"title"`
	Disambiguation string     `json:"disambiguation,omitempty"`
	Country        string     `json:"country,omitempty"`
	Format         string     `json:"format,omitempty"`
	Label          string     `json:"label,omitempty"`
	Year           int        `json:"year,omitempty"`
	Discs          int        `json:"discs"`
	Count          int        `json:"count"`
	Ms             int        `json:"ms"`
	Pressings      int        `json:"pressings"`
	Tracks         []Track    `json:"tracks"`
	pressed        []pressing // every release of this tracklist, to recognise a tracker name (« 1999 », « MFSL »)
}

// pressing is one release of an edition: when, where, on what and by whom it was pressed.
type pressing struct {
	year                                            int
	release, country, format, label, disambiguation string
}

// AlbumDetail is an album with its editions, the most pressed one first.
type AlbumDetail struct {
	MBID       string    `json:"mbid"`
	Title      string    `json:"title"`
	Artist     string    `json:"artist"`
	ArtistMBID string    `json:"artistMbid"`
	Type       string    `json:"type"`
	Secondary  []string  `json:"secondary"`
	Year       int       `json:"year"`
	Editions   []Edition `json:"editions"`
}

// strs reads a field the metadata server sends as a string or as a list of strings.
type strs []string

func (s *strs) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		if one != "" {
			*s = strs{one}
		}
		return nil
	}
	var many []string
	err := json.Unmarshal(b, &many)
	*s = many
	return err
}

// release is one MusicBrainz release in the metadata server's album answer.
type release struct {
	ID, Title, Status, ReleaseDate, Disambiguation string
	Country, Label                                 strs
	Media                                          []struct{ Format string }
	Tracks                                         []struct {
		TrackName                               string
		TrackPosition, MediumNumber, DurationMs int
	}
}

// Album reads an album's releases from the metadata server and groups them into editions.
func (m *Metadata) Album(ctx context.Context, mbid string) (AlbumDetail, error) {
	var res struct {
		ID, Title, Type, ReleaseDate, ArtistID string
		SecondaryTypes                         []string
		Artists                                []struct{ ID, ArtistName string }
		Releases                               []release
	}
	if err := m.get(ctx, "/album/"+url.PathEscape(mbid), &res); err != nil {
		return AlbumDetail{}, err
	}
	al := AlbumDetail{MBID: mbid, Title: res.Title, Type: res.Type, Secondary: res.SecondaryTypes, Year: year(res.ReleaseDate), ArtistMBID: res.ArtistID}
	if al.Secondary == nil {
		al.Secondary = []string{}
	}
	for i, ar := range res.Artists {
		if ar.ID == res.ArtistID || (i == 0 && al.Artist == "") {
			al.Artist, al.ArtistMBID = ar.ArtistName, ar.ID
		}
	}
	official := slices.ContainsFunc(res.Releases, func(r release) bool {
		return r.Status == "Official"
	})
	slices.SortStableFunc(res.Releases, func(a, b release) int {
		return strings.Compare(dateKey(a.ReleaseDate), dateKey(b.ReleaseDate))
	})
	kept := func(r release) bool { return !official || r.Status == "Official" }
	// a release without a tracklist is no edition, unless the album has nothing else
	listed := slices.ContainsFunc(res.Releases, func(r release) bool { return kept(r) && len(r.Tracks) > 0 })
	byList := map[string]int{} // folded tracklist -> index in al.Editions
	for _, r := range res.Releases {
		if !kept(r) || (listed && len(r.Tracks) == 0) {
			continue
		}
		var key strings.Builder
		ed := Edition{Key: r.ID, Title: r.Title, Disambiguation: r.Disambiguation, Year: year(r.ReleaseDate),
			Country: strings.Join(r.Country, ", "), Label: strings.Join(r.Label, ", "), Format: mediaNames(r.Media), Pressings: 1}
		discs := map[int]bool{}
		for _, t := range r.Tracks {
			ed.Tracks = append(ed.Tracks, Track{Disc: max(t.MediumNumber, 1), N: t.TrackPosition, Title: t.TrackName, Ms: t.DurationMs})
			ed.Ms += t.DurationMs
			discs[max(t.MediumNumber, 1)] = true
			fmt.Fprintf(&key, "%d|%s/", max(t.MediumNumber, 1), strings.Join(words(t.TrackName), " "))
		}
		ed.Count, ed.Discs = len(ed.Tracks), len(discs)
		ed.Name = ed.parts().name()
		p := pressing{ed.Year, r.ID, ed.Country, ed.Format, ed.Label, ed.Disambiguation}
		if i, ok := byList[key.String()]; ok {
			al.Editions[i].Pressings++
			al.Editions[i].pressed = append(al.Editions[i].pressed, p)
			continue
		}
		ed.pressed = []pressing{p}
		byList[key.String()] = len(al.Editions)
		al.Editions = append(al.Editions, ed)
	}
	slices.SortStableFunc(al.Editions, func(a, b Edition) int { return b.Pressings - a.Pressings }) // releases are already by date
	if al.Editions == nil {
		al.Editions = []Edition{}
	}
	return al, nil
}

// dateKey sorts unknown dates last, « 0001-01-01 » among them.
func dateKey(d string) string {
	if year(d) == 0 {
		return "9999"
	}
	return d
}

// year is a date's year, 0 when unknown: the metadata server writes an unknown date « 0001-01-01 ».
func year(d string) int {
	if len(d) < 4 {
		return 0
	}
	if y, _ := strconv.Atoi(d[:4]); y >= 1900 {
		return y
	}
	return 0
}

// mediaNames names an edition's media: « CD », « 2 CD », « CD + DVD ».
func mediaNames(media []struct{ Format string }) string {
	var names []string
	count := map[string]int{}
	for _, m := range media {
		f := m.Format
		if f == "" {
			continue
		}
		if count[f] == 0 {
			names = append(names, f)
		}
		count[f]++
	}
	for i, f := range names {
		if count[f] > 1 && f != "Digital Media" { // files, not objects: « 2 Numérique » says nothing
			names[i] = strconv.Itoa(count[f]) + " " + f
		}
	}
	return strings.Join(names, " + ")
}
