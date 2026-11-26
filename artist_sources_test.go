package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const bonIverMBID = "c7d3a1c1-27fb-4eb4-bab2-a0ebaa0ae94e"

var bonIverAlbums = map[string]string{
	"For Emma, Forever Ago": "11111111-0000-4000-8000-000000000001",
	"Bon Iver, Bon Iver":    "11111111-0000-4000-8000-000000000002",
	"22, A Million":         "11111111-0000-4000-8000-000000000003",
	"i,i":                   "11111111-0000-4000-8000-000000000004",
	"Sable, fable":          "11111111-0000-4000-8000-000000000005",
}

// newArtistSourcesApp serves Bon Iver's discography from the metadata server, and Ye's.
func newArtistSourcesApp(t *testing.T) (*fake, *App, http.Handler) {
	_, pf, a, h := newSourcesApp(t)
	ye := newTestMetadata(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artist/"+bonIverMBID {
			http.Redirect(w, r, ye.base+r.URL.Path, http.StatusFound)
			return
		}
		var albums []string
		for title, id := range bonIverAlbums {
			albums = append(albums, `{"Id":"`+id+`","Title":"`+title+`","Type":"Album","ReleaseStatuses":["Official"]}`)
		}
		w.Write([]byte(`{"artistname":"Bon Iver","artistaliases":[],"Albums":[` + strings.Join(albums, ",") + `]}`))
	}))
	t.Cleanup(srv.Close)
	a.meta = NewMetadata(srv.URL)
	a.svc.meta = a.meta
	return pf, a, h
}

type artistSourcesRes struct {
	Query  string
	Albums map[string][]SourceItem
	Others []SourceItem
	Names  []string
}

func getArtistSources(t *testing.T, h http.Handler, path string) artistSourcesRes {
	t.Helper()
	w := call(h, "GET", path, "")
	var res artistSourcesRes
	if err := json.Unmarshal(w.Body.Bytes(), &res); w.Code != 200 || err != nil {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	return res
}

func titlesOf(items []SourceItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestArtistSources(t *testing.T) {
	_, _, h := newArtistSourcesApp(t)
	flat := titlesOf(searchSources(t, h))
	res := getArtistSources(t, h, "/api/artists/"+bonIverMBID+"/sources")
	if res.Query != "Bon Iver" {
		t.Fatalf("query %q", res.Query)
	}
	emma := titlesOf(res.Albums[bonIverAlbums["For Emma, Forever Ago"]])
	if len(emma) != 2 || !slices.Contains(emma, "Bon.Iver.For.Emma.Forever.Ago.2008.WEB.mp3.320kbps-NoTag") {
		t.Fatalf("For Emma: %q", emma)
	}
	if got := res.Albums[bonIverAlbums["Bon Iver, Bon Iver"]]; len(got) != 2 {
		t.Fatalf("Bon Iver, Bon Iver: %q", titlesOf(got))
	}
	if got := res.Albums[bonIverAlbums["Sable, fable"]]; len(got) != 2 {
		t.Fatalf("Sable, fable: %q", titlesOf(got))
	}
	all := slices.Clone(res.Others)
	for _, items := range res.Albums {
		all = append(all, items...)
		last := -1
		for _, title := range titlesOf(items) { // the search's order, quality first, stays
			i := slices.Index(flat, title)
			if i <= last {
				t.Fatalf("order changed: %q", titlesOf(items))
			}
			last = i
		}
	}
	for _, it := range all {
		if strings.Contains(it.Title, "Taylor") || strings.Contains(it.Title, "Boney") || it.Token == "" {
			t.Fatalf("filed: %+v", it)
		}
	}
}

func TestArtistSourcesOthers(t *testing.T) {
	_, _, h := newArtistSourcesApp(t)
	res := getArtistSources(t, h, "/api/artists/"+bonIverMBID+"/sources")
	others := titlesOf(res.Others)
	n := 0
	for _, title := range others {
		if strings.Contains(title, "Дискография") || strings.Contains(title, "Коллекция") {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("discographies in others: %d of %q", n, others)
	}
	for _, items := range res.Albums {
		for _, it := range items {
			if slices.Contains(it.Quality.Flags, "Discographie") {
				t.Fatalf("discography under an album: %q", it.Title)
			}
		}
	}
}

func TestArtistSourcesAlias(t *testing.T) {
	pf, _, h := newArtistSourcesApp(t)
	res := getArtistSources(t, h, "/api/artists/"+yeMBID+"/sources?q=Kanye+West")
	if !strings.Contains(pf.calls[len(pf.calls)-1], "query=Kanye+West") || res.Query != "Kanye West" {
		t.Fatalf("asked %q, query %q", pf.calls, res.Query)
	}
	if !slices.Contains(res.Names, "Kanye West") || !slices.Contains(res.Names, "Ye") {
		t.Fatalf("names %q", res.Names)
	}
}

func TestArtistSourcesErrors(t *testing.T) {
	_, _, plain := newTestApp(t)
	if w := call(plain, "GET", "/api/artists/"+bonIverMBID+"/sources", ""); w.Code != 503 || errCode(w) != "sources.off" {
		t.Fatalf("no Prowlarr: %d %s", w.Code, w.Body)
	}
	pf, _, h := newArtistSourcesApp(t)
	if w := call(h, "GET", "/api/artists/xyz/sources", ""); w.Code != 400 {
		t.Fatalf("bad id: %d", w.Code)
	}
	if w := call(h, "GET", "/api/artists/"+bonIverMBID+"/sources?q=a", ""); w.Code != 400 {
		t.Fatalf("short q: %d", w.Code)
	}
	pf.reply("GET /api/v1/search", 500, `{}`)
	if w := call(h, "GET", "/api/artists/"+bonIverMBID+"/sources", ""); w.Code != 502 {
		t.Fatalf("Prowlarr down: %d %s", w.Code, w.Body)
	}
}

// "Choisir une source" opens the album's page, under its artist: requests and albums say which.
func TestItemsCarryArtist(t *testing.T) {
	_, a, h := newTestApp(t)
	a.meta = newTestMetadata(t)
	a.svc.store.Put(Request{MBID: mbid, Title: "Discovery", ArtistMBID: artistMBID, Status: "waiting"})
	var list struct{ Requests []Item }
	json.Unmarshal(call(h, "GET", "/api/requests", "").Body.Bytes(), &list)
	var page struct{ Results []Item }
	json.Unmarshal(call(h, "GET", "/api/artists/"+yeMBID, "").Body.Bytes(), &page)
	if len(list.Requests) != 1 || list.Requests[0].ArtistMBID != artistMBID || len(page.Results) == 0 || page.Results[0].ArtistMBID != yeMBID {
		t.Fatalf("requests %+v, page %+v", list.Requests, page.Results)
	}
}
