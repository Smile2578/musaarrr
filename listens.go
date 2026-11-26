package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Listens reads how often each album is played on ListenBrainz: open MetaBrainz data with the
// same album ids as Lidarr, behind a free user token.
type Listens struct {
	base, token string
	hc          *http.Client
}

func NewListens(base, token string) *Listens {
	return &Listens{base: strings.TrimRight(base, "/"), token: token, hc: &http.Client{Timeout: 30 * time.Second}}
}

// ListensFor is nil without a token: since November 2026 ListenBrainz refuses most artists without one.
func ListensFor(c Config) *Listens {
	if c.ListenBrainzURL == "off" || c.ListenBrainzToken == "" {
		return nil
	}
	return NewListens(c.ListenBrainzURL, c.ListenBrainzToken)
}

// TopAlbums maps each album of the artist that ListenBrainz knows to its number of listens.
func (l *Listens) TopAlbums(ctx context.Context, artistMBID string) (map[string]int64, error) {
	var rows []struct {
		MBID  string `json:"release_group_mbid"`
		Count int64  `json:"total_listen_count"`
	}
	// The answer carries every album's metadata: 2.6 MB for Ye. Only the counts are kept.
	if err := l.get(ctx, "/1/popularity/top-release-groups-for-artist/"+url.PathEscape(artistMBID), nil, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		if r.Count > 0 {
			out[r.MBID] = r.Count
		}
	}
	return out, nil
}

// lbGroup is one album of the site's statistics.
type lbGroup struct {
	MBID        string   `json:"release_group_mbid"`
	Name        string   `json:"release_group_name"`
	Artist      string   `json:"artist_name"`
	ArtistMBIDs []string `json:"artist_mbids"`
	CaaID       int64    `json:"caa_id"`
	CaaRelease  string   `json:"caa_release_mbid"`
	Listens     int64    `json:"listen_count"`
}

// Sitewide is a page of the albums most listened on all of ListenBrainz over rng ("all_time",
// "this_month", "month" for last month); empty while its statistics are being computed.
func (l *Listens) Sitewide(ctx context.Context, rng string, offset, count int) ([]lbGroup, error) {
	var res struct {
		Payload struct {
			Groups []lbGroup `json:"release_groups"`
		} `json:"payload"`
	}
	q := url.Values{"range": {rng}, "offset": {strconv.Itoa(offset)}, "count": {strconv.Itoa(count)}}
	err := l.get(ctx, "/1/stats/sitewide/release-groups", q, &res)
	return res.Payload.Groups, err
}

type lbTag struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// lbMeta is what ListenBrainz knows of an album: its date, type, cover, artist and tags.
type lbMeta struct {
	ReleaseGroup struct {
		Date       string `json:"date"`
		Type       string `json:"type"`
		CaaID      int64  `json:"caa_id"`
		CaaRelease string `json:"caa_release_mbid"`
	} `json:"release_group"`
	Artist struct {
		Name    string `json:"name"`
		Artists []struct {
			MBID string `json:"artist_mbid"`
		} `json:"artists"`
	} `json:"artist"`
	Tag struct {
		ReleaseGroup []lbTag `json:"release_group"`
		Artist       []lbTag `json:"artist"`
	} `json:"tag"`
}

// GroupMeta reads the metadata of a few albums at once, by album id; unknown ids are left out.
func (l *Listens) GroupMeta(ctx context.Context, mbids []string) (map[string]lbMeta, error) {
	out := map[string]lbMeta{}
	q := url.Values{"release_group_mbids": {strings.Join(mbids, ",")}, "inc": {"artist tag"}}
	err := l.get(ctx, "/1/metadata/release_group/", q, &out)
	return out, err
}

// Popularity reads the listeners and listens of up to 1,000 albums at once, without a token; the
// albums ListenBrainz has no count for are left out. Its counts are not on the scale of the
// per-artist endpoint's: a ranking uses one or the other, never both.
func (l *Listens) Popularity(ctx context.Context, mbids []string) (map[string][2]int64, error) {
	var rows []struct {
		MBID    string `json:"release_group_mbid"`
		Users   *int64 `json:"total_user_count"`
		Listens *int64 `json:"total_listen_count"`
	}
	body, err := json.Marshal(map[string][]string{"release_group_mbids": mbids})
	if err != nil {
		return nil, err
	}
	if err := l.call(ctx, "POST", "/1/popularity/release-group", nil, body, &rows); err != nil {
		return nil, err
	}
	out := make(map[string][2]int64, len(rows))
	for _, r := range rows {
		if r.Users != nil && r.Listens != nil {
			out[r.MBID] = [2]int64{*r.Users, *r.Listens}
		}
	}
	return out, nil
}

// lbUserAgent names Musaarr and where to reach it, as ListenBrainz asks of every client.
const lbUserAgent = "Musaarr ( https://github.com/smile2578/musaarrr )"

// lbBusy is a 429: ListenBrainz (or Wikidata) says how long to wait.
type lbBusy struct{ wait time.Duration }

func (e lbBusy) Error() string {
	return fmt.Sprintf("too many calls, wait %s", e.wait)
}

func (l *Listens) get(ctx context.Context, path string, q url.Values, out any) error {
	return l.call(ctx, "GET", path, q, nil, out)
}

func (l *Listens) call(ctx context.Context, method, path string, q url.Values, body []byte, out any) error {
	u := l.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", lbUserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if l.token != "" {
		req.Header.Set("Authorization", "Token "+l.token)
	}
	resp, err := l.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusTooManyRequests:
		wait, err := strconv.ParseFloat(resp.Header.Get("X-RateLimit-Reset-In"), 64)
		if err != nil || wait <= 0 {
			wait = 10
		}
		return lbBusy{time.Duration(wait * float64(time.Second))}
	case http.StatusOK:
		return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
	}
	return fmt.Errorf("ListenBrainz : HTTP %d", resp.StatusCode)
}
