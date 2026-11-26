package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

func newTestProwlarr(t *testing.T) (*fake, *Prowlarr) {
	f := newFake(t)
	b, err := os.ReadFile("testdata/prowlarr_search_bon_iver.json")
	if err != nil {
		t.Fatal(err)
	}
	f.reply("GET /api/v1/search", 200, string(b))
	f.reply("POST /api/v1/search", 200, `{}`)
	return f, NewProwlarr(f.srv.URL, "secret-key")
}

func TestProwlarrSearch(t *testing.T) {
	f, p := newTestProwlarr(t)
	hits, err := p.Search(ctx, "Bon Iver")
	if err != nil || len(hits) != 19 {
		t.Fatalf("got %d hits, %v", len(hits), err)
	}
	if q := f.calls[0]; q != "GET /api/v1/search?categories=3000&categories=3010&categories=3040&limit=100&query=Bon+Iver&type=search" {
		t.Fatalf("query %s", q)
	}
	if hits[1].Seeders != 11 || hits[1].Indexer != "Indexer A" || hits[1].IndexerID != 2 || hits[1].Size != 390778775 {
		t.Fatalf("hit %+v", hits[1])
	}
	h := hits[8]
	if !strings.HasPrefix(h.Title, "(Indie Folk | Rock) [WEB] Bon Iver - Sable") || len(h.Categories) != 2 || h.Categories[0] != 3040 || h.Categories[1] != 101706 {
		t.Fatalf("hit %+v", h)
	}
	if h.GUID != "REDACTED" || h.PublishDate.Year() != 2025 {
		t.Fatalf("hit %+v", h)
	}
}

func TestProwlarrGrab(t *testing.T) {
	f, p := newTestProwlarr(t)
	if err := p.Grab(ctx, "g1", 3); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies["POST /api/v1/search"]; len(b) != 1 || b[0] != `{"guid":"g1","indexerId":3}` {
		t.Fatalf("body %v", b)
	}
	f.reply("POST /api/v1/search", 404, `{"message":"gone"}`)
	if err := p.Grab(ctx, "g1", 3); !errors.Is(err, ErrSourceExpired) {
		t.Fatalf("404: %v", err)
	}
	f.reply("POST /api/v1/search", 409, `{"message":"qbit"}`)
	if err := p.Grab(ctx, "g1", 3); !errors.Is(err, ErrSourceRefused) {
		t.Fatalf("409: %v", err)
	}
}

func TestProwlarrBadKey(t *testing.T) {
	f := newFake(t)
	f.reply("GET /api/v1/search", 200, `[]`)
	p := NewProwlarr(f.srv.URL, "wrong-key") // the fake answers 401 to any other key
	if _, err := p.Search(ctx, "x"); !errors.Is(err, ErrProwlarrKey) {
		t.Fatalf("got %v", err)
	}
}

func TestProwlarrErrorHidesKey(t *testing.T) {
	f, p := newTestProwlarr(t)
	f.handle("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request, _ string) {
		http.Error(w, "bad key secret-key in "+r.URL.String(), 500)
	})
	_, err := p.Search(ctx, "x")
	if err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("got %v", err)
	}
}

// A 404 on a search means a wrong PROWLARR_URL, not an expired result.
func TestProwlarrSearchNotFoundIsNotExpiry(t *testing.T) {
	f, p := newTestProwlarr(t)
	f.reply("GET /api/v1/search", 404, `{"message":"Not found"}`)
	if _, err := p.Search(ctx, "x"); err == nil || errors.Is(err, ErrSourceExpired) || errors.Is(err, ErrSourceRefused) {
		t.Fatalf("got %v", err)
	}
}

// Prowlarr's own error text can quote a tracker URL with a passkey: only the status is kept.
func TestProwlarrErrorKeepsOnlyStatus(t *testing.T) {
	f, p := newTestProwlarr(t)
	f.reply("GET /api/v1/search", 500, `{"message":"https://tracker.example/dl?passkey=abcdef failed"}`)
	_, err := p.Search(ctx, "x")
	if err == nil || strings.Contains(err.Error(), "passkey") || !strings.Contains(err.Error(), "500") {
		t.Fatalf("got %v", err)
	}
}

func TestProwlarrFetchStaysOnProwlarr(t *testing.T) {
	f, p := newTestProwlarr(t)
	var got string
	f.handle("GET /3/download", func(w http.ResponseWriter, r *http.Request, _ string) {
		got = r.URL.RawQuery
		w.Write([]byte("d4:infod4:name1:a6:lengthi1eee"))
	})
	b, err := p.Fetch(ctx, "http://elsewhere.example:9696/3/download?apikey=k&link=abc")
	if err != nil || string(b) != "d4:infod4:name1:a6:lengthi1eee" || got != "apikey=k&link=abc" {
		t.Fatalf("got %q %q %v", b, got, err)
	}
	f.handle("GET /4/download", func(w http.ResponseWriter, r *http.Request, _ string) {
		http.Redirect(w, r, "magnet:?xt=urn:btih:abc", http.StatusFound)
	})
	if _, err := p.Fetch(ctx, "http://prowlarr/4/download?x=1"); !errors.Is(err, ErrMagnet) {
		t.Fatalf("magnet: %v", err)
	}
	f.handle("GET /5/download", func(w http.ResponseWriter, r *http.Request, _ string) {
		http.Redirect(w, r, "https://tracker.example/dl/passkey", http.StatusFound)
	})
	if _, err := p.Fetch(ctx, "http://prowlarr/5/download"); !errors.Is(err, ErrNoTorrent) {
		t.Fatalf("redirect to a tracker: %v", err)
	}
	f.handle("GET /6/download", func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Write([]byte(`<?xml version="1.0"?><nzb></nzb>`))
	})
	if _, err := p.Fetch(ctx, "http://prowlarr/6/download"); !errors.Is(err, ErrNZB) {
		t.Fatalf("nzb: %v", err)
	}
	if _, err := p.Fetch(ctx, ""); err == nil {
		t.Fatal("empty link accepted")
	}
}
