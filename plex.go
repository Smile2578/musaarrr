package main

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Plex keeps an in-memory index of the music library's albums, keyed by normKey.
type Plex struct {
	base, token string
	hc          *http.Client
	mu          sync.RWMutex
	machineID   string
	index       map[string]string // normKey -> ratingKey
	artists     map[string]int    // artist name -> albums held
	albums      map[string]LibAlbum
	lib         Lib
	tried       time.Time // last reload attempt, failed or not: a down Plex is not hammered
}

func NewPlex(base, token string) *Plex {
	return &Plex{base: strings.TrimRight(base, "/"), token: token, hc: &http.Client{Timeout: 20 * time.Second, CheckRedirect: noRedirect}}
}

func (p *Plex) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", p.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Plex-Token", p.token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.hc.Do(req)
	if err != nil {
		return fmt.Errorf("plex injoignable: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("plex %s: %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (p *Plex) Reload(ctx context.Context) error {
	p.mu.Lock()
	p.tried = time.Now()
	p.mu.Unlock()
	var id struct {
		MediaContainer struct {
			MachineIdentifier string `json:"machineIdentifier"`
		}
	}
	var sections struct {
		MediaContainer struct {
			Directory []struct{ Key, Type string }
		}
	}
	if err := p.get(ctx, "/identity", &id); err != nil {
		return err
	}
	if err := p.get(ctx, "/library/sections", &sections); err != nil {
		return err
	}
	key := ""
	for _, d := range sections.MediaContainer.Directory {
		if d.Type == "artist" {
			key = d.Key
			break
		}
	}
	if key == "" {
		return errors.New("no music library in Plex")
	}
	var all struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey, Title, ParentTitle, Thumb string
				Year, ViewCount, LeafCount           int
				AddedAt, LastViewedAt                int64
				Genre                                []struct{ Tag string }
				// encoding/json matches keys without case: without these tags Plex's "guid" string (plex://album/…)
				// lands in the "Guid" array that includeGuids=1 adds, and the whole library fails to load
				PlexGUID string                `json:"guid"`
				Guid     []struct{ ID string } `json:"Guid"`
			}
		}
	}
	// shortcut: one unpaged call for the whole library, page it if the library grows past ~10k albums.
	if err := p.get(ctx, "/library/sections/"+url.PathEscape(key)+"/all?type=9&includeGuids=1", &all); err != nil {
		return err
	}
	kinds, err := p.albumKinds(ctx, key)
	if err != nil { // the library still loads, without its shelves by type
		log.Printf("Plex, album types: %v", err)
	}
	index := make(map[string]string, len(all.MediaContainer.Metadata))
	artists := map[string]int{}
	albums := make(map[string]LibAlbum, len(all.MediaContainer.Metadata))
	list := make([]LibAlbum, 0, len(all.MediaContainer.Metadata))
	for _, m := range all.MediaContainer.Metadata {
		index[normKey(m.ParentTitle, m.Title)] = m.RatingKey
		artists[m.ParentTitle]++
		al := LibAlbum{Key: m.RatingKey, Title: m.Title, Artist: m.ParentTitle, Year: m.Year, Added: m.AddedAt,
			Played: m.LastViewedAt, Plays: m.ViewCount, Tracks: m.LeafCount, Kind: kinds[m.RatingKey], thumb: m.Thumb}
		if m.Thumb != "" { // the version busts the browser's copy when the artwork changes
			al.Cover = "/api/library/" + url.PathEscape(m.RatingKey) + "/cover?v=" + url.QueryEscape(path.Base(m.Thumb))
		}
		for _, g := range m.Genre {
			al.Genres = append(al.Genres, g.Tag)
		}
		for _, g := range m.Guid {
			if id, ok := strings.CutPrefix(g.ID, "mbid://"); ok {
				al.mbid = id
			}
		}
		albums[m.RatingKey] = al
		list = append(list, al)
	}
	lib, err := encodeJSON(map[string]any{"albums": list, "plex": plexPrefix(id.MediaContainer.MachineIdentifier)})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.machineID, p.index, p.artists, p.albums, p.lib = id.MediaContainer.MachineIdentifier, index, artists, albums, lib
	p.mu.Unlock()
	return nil
}

// Photo asks Plex for a square thumbnail of an artwork path, falling back to the artwork itself;
// the token travels in a header, never in a URL.
func (p *Plex) Photo(ctx context.Context, thumb string, px int) (*http.Response, error) {
	n := fmt.Sprint(px)
	q := url.Values{"width": {n}, "height": {n}, "minSize": {"1"}, "upscale": {"1"}, "url": {thumb}}
	var last error
	for _, u := range []string{p.base + "/photo/:/transcode?" + q.Encode(), p.base + thumb} {
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Plex-Token", p.token)
		resp, err := p.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("plex injoignable: %w", errors.Unwrap(err))
		}
		if resp.StatusCode == 200 {
			return resp, nil
		}
		resp.Body.Close()
		last = fmt.Errorf("plex, image: %d", resp.StatusCode)
	}
	return nil, last
}

// albumKinds gives each album its type. Plex leaves Format and Subformat out of its album list (only an
// album's own page has them) but filters on them: one call per value, Single, EP, Live, Compilation…
func (p *Plex) albumKinds(ctx context.Context, section string) (map[string]string, error) {
	kinds := map[string]string{}
	for _, field := range []string{"subformat", "format"} { // format last: a live EP goes with the singles
		var values struct {
			MediaContainer struct{ Directory []struct{ Key, Title string } }
		}
		if err := p.get(ctx, "/library/sections/"+url.PathEscape(section)+"/"+field+"?type=9", &values); err != nil {
			return kinds, err
		}
		for _, v := range values.MediaContainer.Directory {
			tag := []struct{ Tag string }{{v.Title}}
			kind := albumKind(nil, tag)
			if field == "format" {
				kind = albumKind(tag, nil)
			}
			if kind == "" { // Album, Demo: no shelf of their own
				continue
			}
			var page struct {
				MediaContainer struct{ Metadata []struct{ RatingKey string } }
			}
			if err := p.get(ctx, "/library/sections/"+url.PathEscape(section)+"/all?type=9&"+field+"="+url.QueryEscape(v.Key), &page); err != nil {
				return kinds, err
			}
			for _, m := range page.MediaContainer.Metadata {
				kinds[m.RatingKey] = kind
			}
		}
	}
	return kinds, nil
}

// albumKind reads the type Plex gives an album (musictag tags it): a single or an EP by its
// format, else live, compilation, remix or soundtrack by its subformat; "" for a plain album.
func albumKind(format, subformat []struct{ Tag string }) string {
	for _, f := range format {
		if t := strings.ToLower(f.Tag); t == "ep" || t == "single" {
			return "single"
		}
	}
	for _, f := range subformat {
		switch t := strings.ToLower(f.Tag); t {
		case "live", "compilation", "remix", "soundtrack":
			return t
		}
	}
	return ""
}

// LibAlbum is one album of the Plex library, as the Bibliothèque tab shows it.
type LibAlbum struct {
	Key    string   `json:"key"`
	Title  string   `json:"title"`
	Artist string   `json:"artist"`
	Year   int      `json:"year,omitempty"`
	Added  int64    `json:"added,omitempty"`  // Unix seconds
	Played int64    `json:"played,omitempty"` // last listen, Unix seconds
	Plays  int      `json:"plays,omitempty"`
	Tracks int      `json:"tracks,omitempty"`
	Genres []string `json:"genres,omitempty"`
	Kind   string   `json:"kind,omitempty"` // single, live, compilation, remix, soundtrack; "" for an album
	Cover  string   `json:"cover,omitempty"`
	thumb  string   // Plex's path: fetched with the token, never sent
	mbid   string   // MusicBrainz id Plex tagged it with, if any
}

// Lib is the library answer, encoded once per reload: the tab fetches it whole.
type Lib struct {
	Body, Gz []byte
	Tag      string
}

func encodeJSON(v any) (Lib, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return Lib{}, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(body)
	zw.Close()
	sum := sha256.Sum256(body)
	return Lib{Body: body, Gz: buf.Bytes(), Tag: `"` + hex.EncodeToString(sum[:8]) + `"`}, nil
}

// Library is the encoded library; false until Plex has answered once.
func (p *Plex) Library() (Lib, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lib, p.albums != nil
}

func (p *Plex) Album(key string) (LibAlbum, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	al, ok := p.albums[key]
	return al, ok
}

// Mbid is the MusicBrainz id Plex holds for an album, or "".
func (p *Plex) Mbid(key string) string {
	al, _ := p.Album(key)
	return al.mbid
}

func plexPrefix(machineID string) string {
	return "https://app.plex.tv/desktop#!/server/" + machineID + "/details?key="
}

func (p *Plex) Find(artist, title string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if k := p.index[normKey(artist, title)]; k != "" {
		return k
	}
	// « Bon Iver, Bon Iver » on MusicBrainz is tagged « Bon Iver »: a title said twice is said once.
	if a, b, ok := strings.Cut(title, ","); ok && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) {
		return p.index[normKey(artist, a)]
	}
	return ""
}

// Artists counts the albums Plex holds under each artist name.
func (p *Plex) Artists() map[string]int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return maps.Clone(p.artists)
}

func (p *Plex) Age() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return time.Since(p.tried)
}

func (p *Plex) URL(ratingKey string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return plexPrefix(p.machineID) + url.QueryEscape("/library/metadata/"+ratingKey)
}

var accents = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "æ", "ae", "ç", "c",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ì", "i", "í", "i", "î", "i", "ï", "i",
	"ñ", "n", "ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "œ", "oe",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y", "ß", "ss",
)

var editionWords = []string{"deluxe", "remaster", "edition", "expanded", "anniversary", "bonus", "version"}

// normKey makes "Björk", "Homogenic (Remastered 2011)" and "bjork", "Homogenic" equal.
// shortcut: matches on artist+title only (no file-path check), add a track-path check if false matches show up.
func normKey(artist, title string) string {
	return keep(accents.Replace(strings.ToLower(artist))) + "|" + keep(stripEdition(accents.Replace(strings.ToLower(title))))
}

func stripEdition(t string) string {
	for {
		t = strings.TrimSpace(t)
		if t == "" || (!strings.HasSuffix(t, ")") && !strings.HasSuffix(t, "]")) {
			return t
		}
		open := strings.LastIndexAny(t, "([")
		if open <= 0 {
			return t
		}
		inner := t[open:]
		hit := false
		for _, w := range editionWords {
			hit = hit || strings.Contains(inner, w)
		}
		if !hit {
			return t
		}
		t = t[:open]
	}
}

func keep(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// Owned is the version of an album the user has in Plex: its tracks, and the first track's quality.
type Owned struct {
	Tracks []Track `json:"tracks"`
	Codec  string  `json:"codec,omitempty"`
	Bits   int     `json:"bits,omitempty"`
	Rate   int     `json:"rate,omitempty"`
}

// Version reads an album's tracks from Plex, then the first one's audio stream (the album
// listing carries no bit depth).
func (p *Plex) Version(ctx context.Context, key string) (Owned, error) {
	var kids struct {
		MediaContainer struct {
			Metadata []struct {
				RatingKey, Title             string
				Index, ParentIndex, Duration int
			}
		}
	}
	if err := p.get(ctx, "/library/metadata/"+url.PathEscape(key)+"/children", &kids); err != nil {
		return Owned{}, err
	}
	var v Owned
	for _, t := range kids.MediaContainer.Metadata {
		v.Tracks = append(v.Tracks, Track{Disc: max(t.ParentIndex, 1), N: t.Index, Title: t.Title, Ms: t.Duration})
	}
	if len(v.Tracks) == 0 {
		return v, errors.New("empty album in Plex")
	}
	var one struct {
		MediaContainer struct {
			Metadata []struct {
				Media []struct {
					AudioCodec string
					Part       []struct {
						Stream []struct {
							StreamType, BitDepth, SamplingRate int
							Codec                              string
						}
					}
				}
			}
		}
	}
	if err := p.get(ctx, "/library/metadata/"+url.PathEscape(kids.MediaContainer.Metadata[0].RatingKey), &one); err != nil {
		return v, nil // the tracks are worth showing without their quality
	}
	for _, m := range one.MediaContainer.Metadata {
		for _, md := range m.Media {
			v.Codec = md.AudioCodec
			for _, pt := range md.Part {
				for _, s := range pt.Stream {
					if s.StreamType == 2 {
						v.Codec, v.Bits, v.Rate = cmp.Or(s.Codec, v.Codec), s.BitDepth, s.SamplingRate
						return v, nil
					}
				}
			}
		}
	}
	return v, nil
}
