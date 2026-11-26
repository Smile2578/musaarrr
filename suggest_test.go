package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

const yeezus = "11111111-2222-3333-4444-555555555555"

// Plex holds Ye's Late Registration (as a deluxe edition) and Daft Punk; Lidarr's lookup only knows Ye.
func newSuggestApp(t *testing.T) (*App, func() (bool, []Item)) {
	quickPasses(t, 0)
	f, a, h := newTestApp(t)
	f.reply("GET /api/v1/artist/lookup", 200, `[{"artistName":"Ye","foreignArtistId":"`+yeMBID+`"}]`)
	a.meta = newTestMetadata(t)
	a.svc.meta = a.meta
	a.listens = newTestListens(t, 200, 0)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[
	 {"ratingKey":"55","title":"Late Registration (Deluxe Edition)","parentTitle":"Ye"},
	 {"ratingKey":"56","title":"Discovery","parentTitle":"Daft Punk"}]}}`)
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	a.svc.plex = p
	return a, func() (bool, []Item) {
		w := call(h, "GET", "/api/suggestions", "")
		var res struct {
			Ready   bool
			Results []Item
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); w.Code != 200 || err != nil {
			t.Fatalf("got %d %s", w.Code, w.Body)
		}
		return res.Ready, res.Results
	}
}

func TestSuggestions(t *testing.T) {
	a, get := newSuggestApp(t)
	if ready, got := get(); ready || len(got) != 0 {
		t.Fatalf("before the first pass: %v %+v", ready, got)
	}
	a.refreshSuggestions(ctx)
	ready, got := get()
	// Late Registration is in Plex, Power is a single, Daft Punk is not Ye: Yeezus is left.
	if !ready || len(got) != 1 || got[0].Title != "Yeezus" || got[0].Listens != 2000000 || got[0].ArtistMBID != yeMBID || got[0].Cover != "/api/cover/"+yeezus {
		t.Fatalf("got %+v", got)
	}
}

// Asked for since the last pass: no longer a suggestion.
func TestSuggestionsDropRequested(t *testing.T) {
	a, get := newSuggestApp(t)
	a.refreshSuggestions(ctx)
	a.svc.store.Put(Request{MBID: yeezus, Title: "Yeezus", Status: "searching"})
	if _, got := get(); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// Plex files Ye under "Kanye West", a name MusicBrainz keeps among Ye's aliases.
func TestSuggestionsUnderFormerName(t *testing.T) {
	f, a, h := newTestApp(t)
	f.reply("GET /api/v1/artist/lookup", 200, `[{"artistName":"Ye","foreignArtistId":"`+yeMBID+`"}]`)
	a.meta = newTestMetadata(t)
	a.svc.meta = a.meta
	a.listens = newTestListens(t, 200, 0)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[{"ratingKey":"55","title":"Late Registration","parentTitle":"Kanye West"}]}}`)
	p.Reload(ctx)
	a.svc.plex = p
	a.refreshSuggestions(ctx)
	var res struct{ Results []Item }
	json.Unmarshal(call(h, "GET", "/api/suggestions", "").Body.Bytes(), &res)
	if len(res.Results) != 1 || res.Results[0].Title != "Yeezus" {
		t.Fatalf("got %+v", res.Results)
	}
}

func quickPasses(t *testing.T, pause time.Duration) {
	old := suggestPause
	suggestPause = pause
	t.Cleanup(func() { suggestPause = old })
}

// ListenBrainz allows 30 calls per 10 s and Lidarr slows under a burst: the pass waits between artists.
func TestSuggestionsPaced(t *testing.T) {
	a, _ := newSuggestApp(t)
	quickPasses(t, 80*time.Millisecond)
	start := time.Now()
	a.refreshSuggestions(ctx) // Ye, then Daft Punk
	if d := time.Since(start); d < 80*time.Millisecond {
		t.Fatalf("no pause between the artists: %v", d)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	start = time.Now()
	a.refreshSuggestions(cancelled)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("a stopped app still waits: %v", d)
	}
}
