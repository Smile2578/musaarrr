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
	"strings"
	"time"
)

var (
	ErrSourceExpired = errors.New("result expired")        // Prowlarr keeps a search 30 min
	ErrSourceRefused = errors.New("refused by the client") // the download client said no (already there?)
	ErrProwlarrKey   = errors.New("Prowlarr key refused")
	ErrMagnet        = errors.New("lien magnet, sans fichier .torrent")
	ErrNZB           = errors.New("source Usenet")
	ErrNoTorrent     = errors.New("no .torrent file")
)

// Hit is one release from Prowlarr's search; its guid never leaves the server.
type Hit struct {
	GUID        string
	IndexerID   int
	Indexer     string
	Title       string
	Size        int64
	Seeders     int
	PublishDate time.Time
	Categories  []int
	DownloadURL string // Prowlarr's own link to the .torrent; never leaves the server either
}

type Prowlarr struct {
	base, key string
	hc        *http.Client
}

func NewProwlarr(base, key string) *Prowlarr {
	return &Prowlarr{base: strings.TrimRight(base, "/"), key: key, hc: &http.Client{}}
}

// Search asks every music indexer; audiobooks (3030) are left out by category.
func (p *Prowlarr) Search(ctx context.Context, query string) ([]Hit, error) {
	q := url.Values{"query": {query}, "type": {"search"}, "categories": {"3000", "3010", "3040"}, "limit": {"100"}}
	var res []struct {
		GUID        string    `json:"guid"`
		IndexerID   int       `json:"indexerId"`
		Indexer     string    `json:"indexer"`
		Title       string    `json:"title"`
		Size        int64     `json:"size"`
		Seeders     int       `json:"seeders"`
		PublishDate time.Time `json:"publishDate"`
		DownloadURL string    `json:"downloadUrl"`
		Categories  []struct {
			ID int `json:"id"`
		} `json:"categories"`
	}
	if err := p.do(ctx, "GET", "/api/v1/search", q, nil, &res); err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(res))
	for _, r := range res {
		h := Hit{GUID: r.GUID, IndexerID: r.IndexerID, Indexer: r.Indexer, Title: r.Title, Size: r.Size, Seeders: r.Seeders, PublishDate: r.PublishDate, DownloadURL: r.DownloadURL}
		for _, c := range r.Categories {
			h.Categories = append(h.Categories, c.ID)
		}
		hits = append(hits, h)
	}
	return hits, nil
}

// Grab sends a release to the download client, as Prowlarr's own interface does.
func (p *Prowlarr) Grab(ctx context.Context, guid string, indexerID int) error {
	err := p.do(ctx, "POST", "/api/v1/search", nil, map[string]any{"guid": guid, "indexerId": indexerID}, nil)
	var se *StatusError
	if errors.As(err, &se) {
		switch se.Code {
		case 404: // Prowlarr's 30-min cache of the search expired
			return ErrSourceExpired
		case 409:
			return ErrSourceRefused
		}
	}
	return err
}

// Fetch downloads a .torrent through Prowlarr's download link, always from the Prowlarr Musaarr
// is set up with (the link's own host is ignored), following no redirect: a magnet or a tracker's
// address is never reached from here.
func (p *Prowlarr) Fetch(ctx context.Context, link string) ([]byte, error) {
	u, err := url.Parse(link)
	if err != nil || link == "" {
		return nil, ErrNoTorrent
	}
	base, err := url.Parse(p.base)
	if err != nil {
		return nil, err
	}
	u.Scheme, u.Host, u.User = base.Scheme, base.Host, nil
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", p.key)
	hc := *p.hc
	hc.CheckRedirect = noRedirect
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prowlarr injoignable: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		if strings.HasPrefix(strings.ToLower(resp.Header.Get("Location")), "magnet:") {
			return nil, ErrMagnet
		}
		return nil, ErrNoTorrent // Prowlarr hands the tracker's own link: not followed
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return nil, ErrProwlarrKey
	case resp.StatusCode != 200:
		return nil, &StatusError{resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if t := bytes.TrimSpace(b[:min(len(b), 512)]); bytes.HasPrefix(t, []byte("<?xml")) || bytes.Contains(t, []byte("<nzb")) {
		return nil, ErrNZB
	}
	return b, nil
}

// StatusError is an answer from Prowlarr outside 2xx. Its body is left out: it can quote a
// tracker URL with a passkey, and errors end up in the logs.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("prowlarr %d", e.Code) }

func (p *Prowlarr) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	u := p.base + path
	if q != nil {
		u += "?" + q.Encode()
	}
	if _, ok := ctx.Deadline(); !ok { // a search sets its own, longer deadline
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", p.key)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return fmt.Errorf("prowlarr injoignable: %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	switch resp.StatusCode {
	case 200, 201, 202, 204:
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	case 401, 403:
		return ErrProwlarrKey
	}
	return &StatusError{resp.StatusCode}
}
