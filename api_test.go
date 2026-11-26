package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func newTestApp(t *testing.T) (*fake, *App, http.Handler) {
	f, s := newTestService(t)
	a := NewApp(s)
	a.setReady("")
	return f, a, withSecurity(testAuth(t), a.Routes())
}

func call(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Tailscale-User-Login", "a@b")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return do(h, r)
}

func errMsg(w *httptest.ResponseRecorder) string {
	var e struct{ Error string }
	json.Unmarshal(w.Body.Bytes(), &e)
	return e.Error
}

// errCode is the error's code, which the page words in the reader's language.
func errCode(w *httptest.ResponseRecorder) string {
	var e struct{ Code string }
	json.Unmarshal(w.Body.Bytes(), &e)
	return e.Code
}

func TestSearchValidation(t *testing.T) {
	f, _, h := newTestApp(t)
	if w := call(h, "GET", "/api/search?q=a", ""); w.Code != 400 || errCode(w) != "query.tooShort" {
		t.Errorf("short: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/api/search?q="+strings.Repeat("é", 101), ""); w.Code != 400 || errCode(w) != "query.tooLong" {
		t.Errorf("long: %d %s", w.Code, w.Body)
	}
	var term string
	f.handle("GET /api/v1/album/lookup", func(w http.ResponseWriter, r *http.Request, _ string) {
		term = r.URL.Query().Get("term")
		w.Write([]byte(lookupJSON))
	})
	if w := call(h, "GET", "/api/search?q=AC%2FDC%20%26%20Bj%C3%B6rk%20%231", ""); w.Code != 200 || term != "AC/DC & Björk #1" {
		t.Errorf("encoded: %d term %q", w.Code, term)
	}
}

func TestSearchLidarrDown(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("GET /api/v1/album/lookup", 503, `{"message":"SkyHook"}`)
	w := call(h, "GET", "/api/search?q=daft", "")
	if w.Code != 502 || errCode(w) != "lidarr.metadata" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestSearchSortAndStatus(t *testing.T) {
	f, a, h := newTestApp(t)
	f.reply("GET /api/v1/album/lookup", 200, `[
	 {"foreignAlbumId":"11111111-1111-1111-1111-111111111111","title":"Live","albumType":"Album","secondaryTypes":["Live"],"artist":{"artistName":"X"}},
	 {"foreignAlbumId":"22222222-2222-2222-2222-222222222222","title":"Single","albumType":"Single","artist":{"artistName":"X"}},
	 {"foreignAlbumId":"`+mbid+`","title":"Discovery","albumType":"Album","artist":{"artistName":"Daft Punk"}},
	 {"foreignAlbumId":"33333333-3333-3333-3333-333333333333","title":"EP","albumType":"EP","artist":{"artistName":"X"}}]`)
	a.svc.store.Put(Request{MBID: mbid, Status: "downloading", Progress: 42})
	w := call(h, "GET", "/api/search?q=daft", "")
	var res struct{ Results []Item }
	json.Unmarshal(w.Body.Bytes(), &res)
	var titles []string
	for _, it := range res.Results {
		titles = append(titles, it.Title)
	}
	if strings.Join(titles, ",") != "Discovery,EP,Single,Live" {
		t.Fatalf("order %v", titles)
	}
	if res.Results[0].Status != "downloading" || res.Results[0].Progress != 42 || res.Results[1].Status != "" {
		t.Fatalf("status %+v", res.Results)
	}
}

func TestCreateEndpoint(t *testing.T) {
	_, _, h := newTestApp(t)
	body := `{"mbid":"` + mbid + `"}`
	if w := call(h, "POST", "/api/requests", body); w.Code != 201 {
		t.Fatalf("first: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/requests", body); w.Code != 200 {
		t.Fatalf("repeat: %d", w.Code)
	}
	if w := call(h, "POST", "/api/requests", `{"mbid":"../../etc"}`); w.Code != 400 || errCode(w) != "album.invalid" {
		t.Fatalf("invalid: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "/api/requests", strings.NewReader(body))
	r.Header.Set("Tailscale-User-Login", "a@b")
	r.Header.Set("Content-Type", "text/plain")
	if w := do(h, r); w.Code != 415 {
		t.Fatalf("content type: %d", w.Code)
	}
	if w := call(h, "POST", "/api/requests", `{"mbid":"`+strings.Repeat("a", 5000)+`"}`); w.Code != 400 {
		t.Fatalf("big body: %d", w.Code)
	}
	w := call(h, "GET", "/api/requests", "")
	var res struct{ Requests []Item }
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Requests) != 1 || res.Requests[0].RequestedAt == nil {
		t.Fatalf("list %s", w.Body)
	}
}

func TestCreateErrors(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("POST /api/v1/album", 400, `[{"errorMessage":"Album has no releases"}]`)
	if w := call(h, "POST", "/api/requests", `{"mbid":"`+mbid+`"}`); w.Code != 502 || errMsg(w) != "Lidarr turned the request down: Album has no releases" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	f.reply("GET /api/v1/album/lookup", 200, `[]`)
	if w := call(h, "POST", "/api/requests", `{"mbid":"`+mbid+`"}`); w.Code != 404 || errCode(w) != "album.notInLidarr" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestCreateNotReady(t *testing.T) {
	_, a, h := newTestApp(t)
	a.setReady("profil introuvable")
	if w := call(h, "POST", "/api/requests", `{"mbid":"`+mbid+`"}`); w.Code != 503 {
		t.Fatalf("got %d", w.Code)
	}
	if w := call(h, "GET", "/healthz", ""); w.Code != 503 || w.Body.Len() != 0 {
		t.Fatalf("healthz %d %q", w.Code, w.Body)
	}
}

func TestRetryUnknown(t *testing.T) {
	_, _, h := newTestApp(t)
	if w := call(h, "POST", "/api/requests/"+mbid+"/retry", "{}"); w.Code != 404 {
		t.Fatalf("got %d", w.Code)
	}
	if w := call(h, "POST", "/api/requests/xyz/retry", "{}"); w.Code != 400 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestNoSecretsInResponses(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("POST /api/v1/album", 400, `[{"errorMessage":"bad key secret-key"}]`)
	for _, w := range []*httptest.ResponseRecorder{
		call(h, "GET", "/api/search?q=daft", ""),
		call(h, "POST", "/api/requests", `{"mbid":"`+mbid+`"}`),
		call(h, "GET", "/api/requests", ""),
		call(h, "GET", "/healthz", ""),
	} {
		if strings.Contains(w.Body.String(), "secret-key") || strings.Contains(w.Body.String(), "plex-token") {
			t.Fatalf("secret leaked: %s", w.Body)
		}
	}
}

func TestStaticIndex(t *testing.T) {
	_, _, h := newTestApp(t)
	if w := call(h, "GET", "/", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Musaarr") {
		t.Fatalf("got %d", w.Code)
	}
}

func TestSearchFindsArtists(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("GET /api/v1/album/lookup", 200, lookupJSON)
	f.reply("GET /api/v1/artist/lookup", 200, `[{"artistName":"Ye","foreignArtistId":"`+yeMBID+`"}]`)
	var res struct {
		Artists []Artist
		Results []Item
	}
	w := call(h, "GET", "/api/search?q=kanye%20west", "")
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || len(res.Artists) != 1 || res.Artists[0].Name != "Ye" || len(res.Results) != 1 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	f.reply("GET /api/v1/artist/lookup", 503, `{"message":"SkyHook"}`)
	w = call(h, "GET", "/api/search?q=other", "")
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || len(res.Results) != 1 || res.Artists == nil {
		t.Fatalf("artist failure broke album results: %d %s", w.Code, w.Body)
	}
}

// "Middle of Nowhere – Kacey Musgraves": Lidarr's artist search finds nothing by that name, but the
// album found names its artist, and an artist the query names comes before Lidarr's guesses.
func TestSearchNamedArtistFirst(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("GET /api/v1/album/lookup", 200, `[{"foreignAlbumId":"`+mbid+`","title":"Middle of Nowhere","albumType":"Album",
	 "artist":{"foreignArtistId":"11111111-0000-4000-8000-00000000000a","artistName":"Kacey Musgraves"}},
	 {"foreignAlbumId":"22222222-2222-2222-2222-222222222222","title":"Nowhere","albumType":"Album",
	 "artist":{"foreignArtistId":"11111111-0000-4000-8000-00000000000c","artistName":"Ride"}}]`)
	f.reply("GET /api/v1/artist/lookup", 200, `[{"artistName":"Nobody","foreignArtistId":"11111111-0000-4000-8000-00000000000d"},
	 {"artistName":"Middle of Nowhere","foreignArtistId":"11111111-0000-4000-8000-00000000000b"}]`)
	var res struct{ Artists []Artist }
	w := call(h, "GET", "/api/search?q="+url.QueryEscape("Middle of Nowhere – Kacey Musgraves"), "")
	json.Unmarshal(w.Body.Bytes(), &res)
	var names []string
	for _, a := range res.Artists {
		names = append(names, a.Name)
	}
	// The album's artist the query names, then Lidarr's guesses in its order; Ride, an album's artist
	// the query does not name, is left out.
	if w.Code != 200 || strings.Join(names, ",") != "Kacey Musgraves,Nobody,Middle of Nowhere" || res.Artists[0].MBID != "11111111-0000-4000-8000-00000000000a" {
		t.Fatalf("got %d %q", w.Code, names)
	}
}

// "kanye west": Lidarr puts tribute bands and duets first; Ye, found under its former name, goes before
// them. "bon iver": the artist of that exact name leads, artists named only "Bon" keep Lidarr's order.
func TestSearchArtistOrder(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("GET /api/v1/album/lookup", 200, `[]`)
	order := func(q, lookup string) string {
		f.reply("GET /api/v1/artist/lookup", 200, lookup)
		var res struct{ Artists []Artist }
		json.Unmarshal(call(h, "GET", "/api/search?q="+url.QueryEscape(q), "").Body.Bytes(), &res)
		var names []string
		for _, a := range res.Artists {
			names = append(names, a.Name)
		}
		return strings.Join(names, ",")
	}
	a := func(n int, name string) string {
		return fmt.Sprintf(`{"artistName":%q,"foreignArtistId":"11111111-0000-4000-8000-00000000000%d"}`, name, n)
	}
	if got := order("kanye west", "["+a(1, "Kanye West Tribute Band")+","+a(2, "Kanye West & Hatsune Miku")+","+a(3, "Ye")+"]"); got != "Ye,Kanye West Tribute Band,Kanye West & Hatsune Miku" {
		t.Fatalf("kanye west: %s", got)
	}
	if got := order("bon iver", "["+a(1, "Bon")+","+a(2, "Bon Iver")+","+a(3, "Bon")+"]"); got != "Bon Iver,Bon,Bon" {
		t.Fatalf("bon iver: %s", got)
	}
}

// The album page offers the artist's other names when the trackers know it under one.
func TestArtistPageNames(t *testing.T) {
	_, a, h := newTestApp(t)
	a.meta = newTestMetadata(t)
	var res struct{ Names []string }
	json.Unmarshal(call(h, "GET", "/api/artists/"+yeMBID, "").Body.Bytes(), &res)
	if !slices.Contains(res.Names, "Kanye West") {
		t.Fatalf("names %q", res.Names)
	}
}

func TestArtistPage(t *testing.T) {
	_, a, h := newTestApp(t)
	a.meta = newTestMetadata(t)
	w := call(h, "GET", "/api/artists/"+yeMBID, "")
	var res struct {
		Artist  Artist
		Results []Item
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Artist.Name != "Ye" || len(res.Results) != 3 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if c := res.Results[0].Cover; c != "/api/cover/c563d738-3841-31ce-b29d-ec6b40fd1e7b" {
		t.Fatalf("cover %q", c)
	}
	if w := call(h, "GET", "/api/artists/nope", ""); w.Code != 400 {
		t.Fatalf("invalid id: %d", w.Code)
	}
}

func TestCoverRedirect(t *testing.T) {
	f, _, h := newTestApp(t)
	f.reply("GET /api/v1/album/lookup", 200, lookupJSON)
	w := call(h, "GET", "/api/cover/"+mbid, "")
	if w.Code != 302 || w.Header().Get("Location") != "https://imagecache.lidarr.audio/v1/caa/abc/1200.jpg" {
		t.Fatalf("got %d %v", w.Code, w.Header())
	}
	f.reply("GET /api/v1/album/lookup", 200, `[]`)
	if w := call(h, "GET", "/api/cover/00000000-0000-0000-0000-000000000000", ""); w.Code != 404 {
		t.Fatalf("missing cover: %d", w.Code)
	}
}

func TestTimeoutMessage(t *testing.T) {
	err := fmt.Errorf("lidarr injoignable: %w", context.DeadlineExceeded)
	if m := lidarrDown(err, "x"); m != "lidarr.slow" {
		t.Fatalf("got %q", m)
	}
}

// Plex files Ye's albums under "Kanye West": the discography must still show them as owned.
func TestArtistPageMarksAlbumInPlexUnderFormerName(t *testing.T) {
	_, a, h := newTestApp(t)
	a.meta = newTestMetadata(t)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[{"ratingKey":"55","title":"Late Registration","parentTitle":"Kanye West"}]}}`)
	p.Reload(ctx)
	a.svc.plex = p
	w := call(h, "GET", "/api/artists/"+yeMBID, "")
	var res struct{ Results []Item }
	json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Results) == 0 || res.Results[0].Title != "Late Registration" || res.Results[0].Status != "in_plex" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}
