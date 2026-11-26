package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func newSourcesApp(t *testing.T) (*fake, *fake, *App, http.Handler) {
	f, a, h := newTestApp(t)
	pf, p := newTestProwlarr(t)
	a.sources = NewSources(p)
	return f, pf, a, h
}

func searchSources(t *testing.T, h http.Handler) []SourceItem {
	t.Helper()
	w := call(h, "GET", "/api/sources?q=Bon+Iver", "")
	var res struct{ Results []SourceItem }
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || len(res.Results) != 19 {
		t.Fatalf("search: %d %s", w.Code, w.Body)
	}
	return res.Results
}

func TestSourcesAPINotConfigured(t *testing.T) {
	_, _, h := newTestApp(t)
	w := call(h, "GET", "/api/sources?q=Bon+Iver", "")
	if w.Code != 503 || errCode(w) != "sources.off" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestSourcesAPIValidation(t *testing.T) {
	_, _, _, h := newSourcesApp(t)
	if w := call(h, "GET", "/api/sources?q=a", ""); w.Code != 400 || errCode(w) != "query.tooShort" {
		t.Fatalf("short query: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/sources/grab", `{"token":"../x"}`); w.Code != 400 || errCode(w) != "source.invalid" {
		t.Fatalf("bad token: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/sources/grab", ""); w.Code != 415 {
		t.Fatalf("no JSON: %d", w.Code)
	}
}

func TestSourcesAPISearchAndGrab(t *testing.T) {
	_, _, _, h := newSourcesApp(t)
	items := searchSources(t, h)
	w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`"}`)
	var res struct{ Title string }
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Title != items[0].Title {
		t.Fatalf("grab: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`"}`); w.Code != 409 || errCode(w) != "grab.alreadySent" {
		t.Fatalf("second grab: %d %s", w.Code, w.Body)
	}
}

func TestSourcesAPIExpired(t *testing.T) {
	_, _, _, h := newSourcesApp(t)
	w := call(h, "POST", "/api/sources/grab", `{"token":"`+strings.Repeat("0f", 16)+`"}`)
	if w.Code != 410 || errCode(w) != "source.expired" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestSourcesAPIRefused(t *testing.T) {
	_, pf, _, h := newSourcesApp(t)
	items := searchSources(t, h)
	pf.reply("POST /api/v1/search", 409, `{}`)
	w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`"}`)
	if w.Code != 409 || errCode(w) != "grab.refused" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestSourcesAPITimeout(t *testing.T) {
	_, pf, _, h := newSourcesApp(t)
	old := sourcesTimeout
	sourcesTimeout = 50 * time.Millisecond
	t.Cleanup(func() { sourcesTimeout = old })
	pf.handle("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request, _ string) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	})
	w := call(h, "GET", "/api/sources?q=Bon+Iver", "")
	if w.Code != 504 || errCode(w) != "sources.slow" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestSourcesAPIBadKey(t *testing.T) {
	_, _, a, h := newSourcesApp(t)
	f := newFake(t)
	a.sources = NewSources(NewProwlarr(f.srv.URL, "wrong-key"))
	w := call(h, "GET", "/api/sources?q=Bon+Iver", "")
	if w.Code != 502 || errCode(w) != "prowlarr.key" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

// A pick only goes to a request that waits for a source, never to one Musaarr's own search still serves.
func TestSourcesAPIGrabRefusesRequestNotWaiting(t *testing.T) {
	_, pf, a, h := newSourcesApp(t)
	a.svc.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "in_plex", RequestedAt: time.Now()})
	items := searchSources(t, h)
	w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`","mbid":"`+mbid+`"}`)
	if w.Code != 409 || errCode(w) != "grab.notWaiting" || pf.count("POST /api/v1/search") != 0 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

// A linked grab Prowlarr does not confirm is still attached: Lidarr must stop looking either way.
func TestSourcesAPIGrabUnsureStillLinks(t *testing.T) {
	_, pf, a, h := newSourcesApp(t)
	waitingDiscovery(a.svc)
	old := sourcesTimeout
	sourcesTimeout = 50 * time.Millisecond
	t.Cleanup(func() { sourcesTimeout = old })
	items := searchSources(t, h)
	pf.handle("POST /api/v1/search", func(w http.ResponseWriter, r *http.Request, _ string) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	})
	w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`","mbid":"`+mbid+`"}`)
	if w.Code != 504 || errCode(w) != "grab.unsure" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if r, _ := a.svc.store.Get(mbid); r.Status != "picked" || r.Picked != items[0].Title {
		t.Fatalf("request %+v", r)
	}
}

// A search abandoned by the browser (a chip tap started another) is not an error worth a log line.
func TestSourcesAPICancelledSearchLogsNothing(t *testing.T) {
	_, pf, _, h := newSourcesApp(t)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	pf.handle("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request, _ string) { <-r.Context().Done() })
	cctx, cancel := context.WithCancel(ctx)
	r := httptest.NewRequestWithContext(cctx, "GET", "/api/sources?q=Bon+Iver", nil)
	r.Header.Set("Tailscale-User-Login", "a@b")
	time.AfterFunc(30*time.Millisecond, cancel)
	do(h, r)
	if buf.Len() != 0 {
		t.Fatalf("logged %q", buf.String())
	}
}

// Another site open on the tailnet must not start a tracker search with an <img> tag.
func TestCrossSiteGetRefused(t *testing.T) {
	_, pf, _, h := newSourcesApp(t)
	r := httptest.NewRequest("GET", "/api/sources?q=Bon+Iver", nil)
	r.Header.Set("Tailscale-User-Login", "a@b")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if w := do(h, r); w.Code != 403 || pf.count("GET /api/v1/search") != 0 {
		t.Fatalf("cross-site: %d", w.Code)
	}
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if w := do(h, r); w.Code != 200 {
		t.Fatalf("same-origin: %d", w.Code)
	}
}

type filesAnswer struct {
	Files []struct {
		Name  string
		Size  int64
		Audio bool
	}
	Tracks, Discs int
	Image         bool
	Edition       *Match
}

func lateRegSource(t *testing.T, torrent []byte) (*fake, http.Handler, string) {
	t.Helper()
	_, pf, a, h := newSourcesApp(t)
	a.meta = newAlbumMetadata(t, lateRegJSON)
	pf.reply("GET /api/v1/search", 200, `[{"guid":"g","indexerId":1,"indexer":"X","title":"Kanye West - Late Registration (2006 Japan SHM-CD) FLAC","seeders":3,
	 "categories":[{"id":3040}],"downloadUrl":"http://prowlarr.lan/1/download?apikey=secret-key&link=xyz"}]`)
	pf.handle("GET /1/download", func(w http.ResponseWriter, r *http.Request, _ string) { w.Write(torrent) })
	w := call(h, "GET", "/api/sources?q=Late+Registration&year=2005", "")
	var res struct{ Results []SourceItem }
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Results) != 1 || strings.Join(res.Results[0].Hints, ",") != "japan,SHM-CD,pressing:2006" {
		t.Fatalf("search: %s", w.Body)
	}
	return pf, h, res.Results[0].Token
}

func TestSourceFilesMatchEdition(t *testing.T) {
	_, h, tok := lateRegSource(t, multiTorrent("LR/01 Wake Up Mr. West.flac", "LR/02 Heard Em Say.flac", "LR/03 Touch the Sky.flac", "LR/04 Late.flac", "LR/cover.jpg", "LR/LR.log"))
	w := call(h, "POST", "/api/sources/files", `{"token":"`+tok+`","album":"`+lateRegMBID+`"}`)
	var res filesAnswer
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Tracks != 4 || res.Discs != 1 || res.Image || len(res.Files) != 6 || !res.Files[0].Audio || res.Files[4].Audio || res.Files[0].Name != "01 Wake Up Mr. West.flac" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if res.Edition == nil || res.Edition.Key != "r-jp" || !res.Edition.Exact {
		t.Fatalf("edition %s", w.Body)
	}
	for _, leak := range []string{"secret-key", "prowlarr.lan", "xyz", "LR/"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("%s reaches the browser", leak)
		}
	}
	w = call(h, "POST", "/api/sources/files", `{"token":"`+tok+`"}`)
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Edition != nil {
		t.Fatalf("no album, no edition: %s", w.Body)
	}
}

func TestSourceFilesImageAndDiscs(t *testing.T) {
	_, h, tok := lateRegSource(t, multiTorrent("A/CD1/A.flac", "A/CD1/A.cue", "A/CD2/B.flac", "A/CD2/B.cue"))
	w := call(h, "POST", "/api/sources/files", `{"token":"`+tok+`","album":"`+lateRegMBID+`"}`)
	var res filesAnswer
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || !res.Image || res.Discs != 2 || res.Tracks != 0 || res.Edition != nil {
		t.Fatalf("image + cue: %s", w.Body)
	}
}

func TestSourceFilesErrors(t *testing.T) {
	pf, h, tok := lateRegSource(t, nil)
	pf.handle("GET /1/download", func(w http.ResponseWriter, r *http.Request, _ string) {
		http.Redirect(w, r, "magnet:?xt=urn:btih:abc", http.StatusFound)
	})
	w := call(h, "POST", "/api/sources/files", `{"token":"`+tok+`"}`)
	if w.Code != 422 || errCode(w) != "files.magnet" {
		t.Fatalf("magnet: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/sources/files", `{"token":"zz"}`); w.Code != 400 {
		t.Fatalf("bad token: %d", w.Code)
	}
	if w := call(h, "POST", "/api/sources/files", `{"token":"`+strings.Repeat("a", 32)+`"}`); w.Code != 410 {
		t.Fatalf("unknown token: %d", w.Code)
	}
}
