package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	bbMBID    = "a9e30282-5b37-3f92-b897-b9659a1a312b"
	bbLive    = "22222222-0000-4000-8000-000000000002"
	milesMBID = "561d854a-6a28-4aa7-8c99-323e6ce46c2a"
)

// bbTracks is Bitches Brew's tracklist on 2 discs, with « Feio » for the 1999 reissue.
func bbTracks(feio bool) string {
	names := []string{"Pharaoh's Dance", "Bitches Brew", "Spanish Key", "John McLaughlin", "Miles Runs the Voodoo Down", "Sanctuary"}
	if feio {
		names = append(names, "Feio")
	}
	var ts []string
	for i, n := range names {
		ts = append(ts, fmt.Sprintf(`{"trackposition":%d,"mediumnumber":%d,"trackname":%q,"durationms":1000}`, i+1, 1+i/2, n))
	}
	return "[" + strings.Join(ts, ",") + "]"
}

// newAlbumSourcesApp serves Bitches Brew, Miles Davis's discography and a tracker answer for the album.
func newAlbumSourcesApp(t *testing.T) http.Handler {
	_, pf, a, h := newSourcesApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/album/" + bbMBID:
			fmt.Fprintf(w, `{"id":%q,"title":"Bitches Brew","type":"Album","releasedate":"1970-03-30","artistid":%q,
			 "artists":[{"id":%q,"artistname":"Miles Davis"}],"Releases":[
			  {"id":"r-lp","status":"Official","country":["United States"],"label":["Columbia"],"releasedate":"1970-03-30","media":[{"Format":"12\" Vinyl"},{"Format":"12\" Vinyl"}],"tracks":%s},
			  {"id":"r-99","status":"Official","country":["United States"],"label":["Columbia","Legacy"],"releasedate":"1999-11-23","media":[{"Format":"CD"},{"Format":"CD"}],"tracks":%s},
			  {"id":"r-jp","status":"Official","country":["Japan"],"label":["Sony Music Japan"],"releasedate":"2013-06-26","media":[{"Format":"CD"},{"Format":"CD"}],"tracks":%s}]}`,
				bbMBID, milesMBID, milesMBID, bbTracks(false), bbTracks(true), bbTracks(true))
		case "/artist/" + milesMBID:
			fmt.Fprintf(w, `{"artistname":"Miles Davis","artistaliases":[],"Albums":[{"Id":%q,"Title":"Bitches Brew","Type":"Album","ReleaseStatuses":["Official"]},
			 {"Id":%q,"Title":"Bitches Brew Live","Type":"Album","ReleaseStatuses":["Official"]}]}`, bbMBID, bbLive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	a.meta = NewMetadata(srv.URL)
	a.svc.meta = a.meta
	hit := func(title string, seeders int) string {
		return fmt.Sprintf(`{"guid":%q,"indexerId":1,"indexer":"Indexer B","title":%q,"seeders":%d,"categories":[{"id":3040}]}`, title, title, seeders)
	}
	pf.reply("GET /api/v1/search", 200, "["+strings.Join([]string{
		hit("(Jazz-Rock, Fusion) [CD] Miles Davis - Bitches Brew Live - 2011, FLAC (tracks+.cue), lossless", 50),
		hit("Miles.Davis.The.Complete.Bitches.Brew.Sessions.1969.FLAC.[24bit.96khz]-NOTAG", 40),
		hit("(Jazz) [LP][24/96] Miles Davis - Bitches Brew, 2LP - 1970, FLAC (tracks)", 7),
		hit("(Jazz Fusion) [CD] Miles Davis: Bitches Brew (1970, японское издание 2013), FLAC (tracks+.cue), lossless", 9),
		hit("(Jazz Fusion, Avant-Garde Jazz, Jazz-Rock) [CD] Miles Davis - Bitches Brew - 1970 (1999), FLAC (tracks+.cue), lossless", 11),
		hit("(Experimental, Jazz Rock, Fusion) [WEB] The Bitches Brew Band - The Miles Davis Bitches Brew Album - 2026, FLAC (tracks), lossless", 8),
		hit("Rolling Stone Magazines - Greatest 500 Albums of all Time [FLAC 44.1/16]", 21),
	}, ",")+"]")
	return h
}

func TestAlbumSourcesThisAlbumFirst(t *testing.T) {
	h := newAlbumSourcesApp(t)
	w := call(h, "GET", "/api/sources?q=Miles+Davis+Bitches+Brew&year=1970&album="+bbMBID, "")
	var res struct{ Results []SourceItem }
	if err := json.Unmarshal(w.Body.Bytes(), &res); w.Code != 200 || err != nil {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	var got []string
	for _, it := range res.Results {
		ed := ""
		if it.Edition != nil {
			ed = it.Edition.Key
		}
		got = append(got, fmt.Sprintf("%s|%s|%s|%s", it.Title[:24], it.Place, it.Other, ed))
	}
	want := []string{
		"(Jazz Fusion, Avant-Gard|album||r-99", // 16-bit tracks, the most seeded first
		"(Jazz Fusion) [CD] Miles|album||r-99", // the 2013 Japanese pressing of the 1999 reissue
		"(Jazz) [LP][24/96] Miles|album||r-lp", // vinyl after CD, as a Lidarr with vinyl scored down ranks it
		"Miles.Davis.The.Complete|artist||",    // the artist's, not this album: after it, whatever its quality
		"(Jazz-Rock, Fusion) [CD]|other|Bitches Brew Live|",
		"(Experimental, Jazz Rock|||", // another artist's homage: the name comes after theirs
		"Rolling Stone Magazines |||",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The page does not always know the album's year; the server does, and reads later years as pressings.
func TestAlbumSourcesHintsUseTheAlbumYear(t *testing.T) {
	h := newAlbumSourcesApp(t)
	w := call(h, "GET", "/api/sources?q=Miles+Davis+Bitches+Brew&album="+bbMBID, "")
	var res struct{ Results []SourceItem }
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || len(res.Results) == 0 || strings.Join(res.Results[0].Hints, ",") != "pressing:1999" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestAlbumSourcesWithoutAlbumKeepTheirOrder(t *testing.T) {
	h := newAlbumSourcesApp(t)
	w := call(h, "GET", "/api/sources?q=Miles+Davis+Bitches+Brew", "")
	var res struct{ Results []SourceItem }
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || len(res.Results) != 7 || res.Results[0].Place != "" || res.Results[0].Edition != nil ||
		!strings.Contains(res.Results[0].Title, "Complete") { // the free search: quality first, as before
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}
