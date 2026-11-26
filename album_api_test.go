package main

import (
	"encoding/json"
	"testing"
)

type albumAnswer struct {
	Album    Item
	Editions []Edition
	Owned    *struct {
		Tracks  []Track
		Codec   string
		Bits    int
		Rate    int
		Edition *Match
	}
}

func TestAlbumPage(t *testing.T) {
	_, a, h := newTestApp(t)
	a.meta = newAlbumMetadata(t, lateRegJSON)
	w := call(h, "GET", "/api/albums/"+lateRegMBID, "")
	var res albumAnswer
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Album.Title != "Late Registration" || res.Album.Artist != "Ye" || res.Album.Cover != "/api/cover/"+lateRegMBID || len(res.Editions) != 2 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if res.Owned != nil {
		t.Fatalf("no Plex, no owned version: %s", w.Body)
	}
	if w := call(h, "GET", "/api/albums/nope", ""); w.Code != 400 {
		t.Fatalf("invalid id: %d", w.Code)
	}
	if w := call(h, "GET", "/api/albums/00000000-0000-0000-0000-000000000000", ""); w.Code != 502 || errCode(w) != "lidarr.metadata" {
		t.Fatalf("missing album: %d %s", w.Code, w.Body)
	}
}

func TestAlbumPageOwnedVersion(t *testing.T) {
	_, a, h := newTestApp(t)
	a.meta = newAlbumMetadata(t, lateRegJSON)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[{"ratingKey":"55","title":"Late Registration","parentTitle":"Ye"}]}}`)
	pf.reply("GET /library/metadata/55/children", 200, `{"MediaContainer":{"Metadata":[
	 {"ratingKey":"551","title":"Wake Up Mr. West","index":1,"parentIndex":1,"duration":41000},
	 {"ratingKey":"552","title":"Heard 'Em Say","index":2,"parentIndex":1,"duration":204000},
	 {"ratingKey":"553","title":"Touch the Sky","index":3,"parentIndex":1,"duration":237000},
	 {"ratingKey":"554","title":"Late","index":4,"parentIndex":1,"duration":229000}]}}`)
	pf.reply("GET /library/metadata/551", 200, `{"MediaContainer":{"Metadata":[{"Media":[{"Part":[{"Stream":[{"streamType":2,"codec":"flac","bitDepth":16,"samplingRate":44100}]}]}]}]}}`)
	p.Reload(ctx)
	a.svc.plex = p
	w := call(h, "GET", "/api/albums/"+lateRegMBID, "")
	var res albumAnswer
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Album.Status != "in_plex" || res.Owned == nil || len(res.Owned.Tracks) != 4 || res.Owned.Bits != 16 || res.Owned.Rate != 44100 {
		t.Fatalf("got %s", w.Body)
	}
	if e := res.Owned.Edition; e == nil || e.Key != "r-jp" || !e.Exact {
		t.Fatalf("edition %+v", res.Owned.Edition)
	}
}
