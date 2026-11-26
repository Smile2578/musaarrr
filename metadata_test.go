package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const yeMBID = "164f0d73-1234-4e2c-8743-d77bf2191051"

func newTestMetadata(t *testing.T) *Metadata {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/artist/"+yeMBID {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"artistname":"Ye","artistaliases":["カニエ・ウェスト","Kanye West","Kanye","Kanye West","Yeezy","Donda","Ye"],"Albums":[
		 {"Id":"c563d738-3841-31ce-b29d-ec6b40fd1e7b","Title":"Late Registration","Type":"Album","SecondaryTypes":[],"ReleaseStatuses":["Bootleg","Official"],"ReleaseDate":null},
		 {"Id":"7d4bba1d-4759-4d22-89f1-c0cc71f5736e","Title":"Power","Type":"Single","SecondaryTypes":[],"ReleaseStatuses":["Official"],"ReleaseDate":"2010-07-01"},
		 {"Id":"4304c426-faa7-3e3e-ac14-40945a593391","Title":"The Lost Tapes","Type":"Album","SecondaryTypes":["Mixtape/Street"],"ReleaseStatuses":["Bootleg"]},
		 {"Id":"11111111-2222-3333-4444-555555555555","Title":"Yeezus","Type":"Album","SecondaryTypes":null,"ReleaseStatuses":["Official"],"ReleaseDate":"2013-06-18"}]}`))
	}))
	t.Cleanup(srv.Close)
	return NewMetadata(srv.URL)
}

func TestArtistAlbums(t *testing.T) {
	m := newTestMetadata(t)
	name, albums, err := m.ArtistAlbums(ctx, yeMBID)
	if err != nil || name != "Ye" {
		t.Fatal(name, err)
	}
	var titles []string
	for _, a := range albums {
		titles = append(titles, a.Title)
	}
	// bootleg-only releases dropped, studio albums before singles
	if len(albums) != 3 || titles[0] != "Late Registration" || titles[1] != "Yeezus" || titles[2] != "Power" {
		t.Fatalf("got %v", titles)
	}
	if albums[1].Year != 2013 || albums[1].ArtistName != "Ye" || albums[1].Secondary == nil {
		t.Fatalf("got %+v", albums[1])
	}
	if _, _, err := m.ArtistAlbums(ctx, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("404 accepted")
	}
}

func TestNames(t *testing.T) {
	m := newTestMetadata(t)
	got, err := m.Names(ctx, yeMBID)
	if err != nil || !slices.Equal(got, []string{"Ye", "カニエ・ウェスト", "Kanye West", "Kanye", "Yeezy", "Donda"}) {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := m.Names(ctx, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("404 accepted")
	}
}

const lateRegMBID = "c563d738-3841-31ce-b29d-ec6b40fd1e7b"

// lateRegJSON is the shape of /album/{mbid} on Lidarr's metadata server (SkyHook's AlbumResource):
// two pressings of one tracklist, a Japanese edition with a bonus, and a promo.
const lateRegJSON = `{"id":"c563d738-3841-31ce-b29d-ec6b40fd1e7b","title":"Late Registration","type":"Album","secondarytypes":[],
 "releasedate":"2005-08-30","artistid":"164f0d73-1234-4e2c-8743-d77bf2191051","artists":[{"id":"164f0d73-1234-4e2c-8743-d77bf2191051","artistname":"Ye"}],
 "Releases":[
  {"id":"r-us","title":"Late Registration","status":"Official","country":["US"],"label":["Roc-A-Fella"],"releasedate":"2005-08-30","disambiguation":"",
   "media":[{"Format":"CD","Name":"","Position":1}],"trackcount":3,
   "tracks":[{"tracknumber":"1","trackposition":1,"mediumnumber":1,"trackname":"Wake Up Mr. West","durationms":41000},
             {"tracknumber":"2","trackposition":2,"mediumnumber":1,"trackname":"Heard ’Em Say","durationms":204000},
             {"tracknumber":"3","trackposition":3,"mediumnumber":1,"trackname":"Touch the Sky","durationms":237000}]},
  {"id":"r-gb","title":"Late Registration","status":"Official","country":"GB","label":["Roc-A-Fella","Def Jam"],"releasedate":"2005-08-29",
   "media":[{"Format":"CD","Position":1}],"trackcount":3,
   "tracks":[{"tracknumber":"1","trackposition":1,"mediumnumber":1,"trackname":"Wake Up Mr. West","durationms":41500},
             {"tracknumber":"2","trackposition":2,"mediumnumber":1,"trackname":"Heard 'em Say","durationms":204000},
             {"tracknumber":"3","trackposition":3,"mediumnumber":1,"trackname":"Touch The Sky","durationms":237000}]},
  {"id":"r-jp","title":"Late Registration","status":"Official","country":["JP"],"label":["Universal"],"releasedate":"2006-02-22","disambiguation":"bonus tracks",
   "media":[{"Format":"SHM-CD","Position":1}],"trackcount":4,
   "tracks":[{"tracknumber":"1","trackposition":1,"mediumnumber":1,"trackname":"Wake Up Mr. West","durationms":41000},
             {"tracknumber":"2","trackposition":2,"mediumnumber":1,"trackname":"Heard 'Em Say","durationms":204000},
             {"tracknumber":"3","trackposition":3,"mediumnumber":1,"trackname":"Touch the Sky","durationms":237000},
             {"tracknumber":"4","trackposition":4,"mediumnumber":1,"trackname":"Late","durationms":229000}]},
  {"id":"r-promo","title":"Late Registration","status":"Promotion","country":["US"],"media":[{"Format":"Digital Media","Position":1}],"trackcount":1,
   "tracks":[{"tracknumber":"1","trackposition":1,"mediumnumber":1,"trackname":"Touch the Sky","durationms":237000}]}]}`

func newAlbumMetadata(t *testing.T, body string) *Metadata {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/album/"+lateRegMBID {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewMetadata(srv.URL)
}

func TestAlbumEditions(t *testing.T) {
	m := newAlbumMetadata(t, lateRegJSON)
	al, err := m.Album(ctx, lateRegMBID)
	if err != nil {
		t.Fatal(err)
	}
	if al.Title != "Late Registration" || al.Artist != "Ye" || al.ArtistMBID != yeMBID || al.Year != 2005 || al.Type != "Album" {
		t.Fatalf("album %+v", al)
	}
	if len(al.Editions) != 2 {
		t.Fatalf("want 2 editions (promo dropped, US and GB pressings merged), got %+v", al.Editions)
	}
	main, jp := al.Editions[0], al.Editions[1]
	if main.Pressings != 2 || main.Count != 3 || main.Discs != 1 || main.Year != 2005 || main.Format != "CD" || main.Key != "r-gb" {
		t.Fatalf("main edition %+v", main) // the GB pressing came out a day earlier: it leads
	}
	if main.Country != "GB" || main.Label != "Roc-A-Fella, Def Jam" || main.Ms != 41500+204000+237000 {
		t.Fatalf("main edition %+v", main)
	}
	if jp.Country != "JP" || jp.Count != 4 || jp.Format != "SHM-CD" || jp.Disambiguation != "bonus tracks" || jp.Tracks[3] != (Track{Disc: 1, N: 4, Title: "Late", Ms: 229000}) {
		t.Fatalf("japanese edition %+v", jp)
	}
	if _, err := m.Album(ctx, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("404 accepted")
	}
}

func TestAlbumPromoOnly(t *testing.T) {
	m := newAlbumMetadata(t, `{"id":"x","title":"Promo","artists":[{"id":"a","artistname":"A"}],"Releases":[
	 {"id":"p","status":"Promotion","country":[],"media":[{"Format":"CD"}],"trackcount":1,"tracks":[{"trackposition":1,"mediumnumber":1,"trackname":"One","durationms":1000}]}]}`)
	al, err := m.Album(ctx, lateRegMBID)
	if err != nil || len(al.Editions) != 1 || al.Editions[0].Count != 1 {
		t.Fatalf("got %+v %v", al, err)
	}
}

func TestAlbumTracklessReleases(t *testing.T) {
	m := newAlbumMetadata(t, `{"id":"x","title":"A","artists":[{"id":"a","artistname":"A"}],"Releases":[
	 {"id":"d1","status":"Official","releasedate":"2020-01-01","country":[],"media":[{"Format":"Digital Media"}],"trackcount":0,"tracks":[]},
	 {"id":"d2","status":"Official","releasedate":"2020-01-02","country":[],"media":[{"Format":"Digital Media"}],"trackcount":0,"tracks":[]},
	 {"id":"d3","status":"Official","releasedate":"2020-01-03","country":[],"media":[{"Format":"Digital Media"}],"trackcount":0,"tracks":[]},
	 {"id":"cd","status":"Official","releasedate":"2020-01-04","country":["FR"],"media":[{"Format":"CD"}],"trackcount":1,"tracks":[{"trackposition":1,"mediumnumber":1,"trackname":"One","durationms":1000}]}]}`)
	al, err := m.Album(ctx, lateRegMBID)
	if err != nil || len(al.Editions) != 1 || al.Editions[0].Key != "cd" {
		t.Fatalf("got %+v %v", al.Editions, err) // releases without a tracklist are not an edition
	}
	m = newAlbumMetadata(t, `{"id":"x","title":"A","artists":[{"id":"a","artistname":"A"}],"Releases":[
	 {"id":"d1","status":"Official","country":[],"media":[{"Format":"Digital Media"}],"trackcount":0,"tracks":[]}]}`)
	if al, err = m.Album(ctx, lateRegMBID); err != nil || len(al.Editions) != 1 {
		t.Fatalf("got %+v %v", al.Editions, err) // with nothing else, it still names the album's release
	}
}

// A release dated « 0001-01-01 » has no date: it neither leads its edition nor names it with year 1.
// Every pressing of an edition is kept, so a name like « 2006 Japan » finds the edition it is in.
func TestAlbumEditionsKeepEveryPressing(t *testing.T) {
	m := newAlbumMetadata(t, strings.Replace(lateRegJSON, `"releasedate":"2005-08-29"`, `"releasedate":"0001-01-01"`, 1))
	al, err := m.Album(ctx, lateRegMBID)
	if err != nil {
		t.Fatal(err)
	}
	main := al.Editions[0]
	if main.Key != "r-us" || main.Year != 2005 || strings.Contains(main.Name, " 1 ") {
		t.Fatalf("main edition %+v", main)
	}
	if len(main.pressed) != 2 || main.pressed[0].year != 2005 || main.pressed[1].year != 0 || main.pressed[1].country != "GB" {
		t.Fatalf("pressings %+v", main.pressed)
	}
}
