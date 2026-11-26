package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormKey(t *testing.T) {
	if a, b := normKey("Björk", "Homogenic (Remastered 2011)"), normKey("bjork", "Homogenic"); a != b {
		t.Errorf("%q != %q", a, b)
	}
	if k := normKey("AC/DC", "Back in Black [Deluxe Edition]"); k != "acdc|backinblack" {
		t.Errorf("got %q", k)
	}
	if normKey("Daft Punk", "Alive 1997") == normKey("Daft Punk", "Alive 2007") {
		t.Error("distinct albums merged")
	}
	if k := normKey("Sigur Rós", "( )"); k != "sigurros|" {
		t.Errorf("got %q", k)
	}
}

func newTestPlex(t *testing.T) (*fake, *Plex) {
	f := newFake(t)
	f.reply("GET /identity", 200, `{"MediaContainer":{"machineIdentifier":"abc123"}}`)
	f.reply("GET /library/sections", 200, `{"MediaContainer":{"Directory":[{"key":"1","type":"movie"},{"key":"3","type":"artist","title":"Musique"}]}}`)
	f.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[
	 {"ratingKey":"123","title":"Discovery","parentTitle":"Daft Punk"},
	 {"ratingKey":"456","title":"Homogenic (Remastered)","parentTitle":"Björk"}]}}`)
	return f, NewPlex(f.srv.URL, "plex-token")
}

func TestPlexReloadAndFind(t *testing.T) {
	f, p := newTestPlex(t)
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if k := p.Find("Daft Punk", "Discovery"); k != "123" {
		t.Errorf("got %q", k)
	}
	if k := p.Find("Bjork", "Homogenic"); k != "456" {
		t.Errorf("got %q", k)
	}
	if k := p.Find("Daft Punk", "Homework"); k != "" {
		t.Errorf("got %q", k)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "plex-token") {
			t.Errorf("token in URL: %s", c)
		}
	}
	if f.count("GET /library/sections/3/all?type=9") != 1 {
		t.Errorf("calls %v", f.calls)
	}
}

func TestPlexURL(t *testing.T) {
	_, p := newTestPlex(t)
	p.Reload(ctx)
	if u := p.URL("123"); u != "https://app.plex.tv/desktop#!/server/abc123/details?key=%2Flibrary%2Fmetadata%2F123" {
		t.Errorf("got %s", u)
	}
}

func TestPlexNoMusicSection(t *testing.T) {
	f, p := newTestPlex(t)
	f.reply("GET /library/sections", 200, `{"MediaContainer":{"Directory":[{"key":"1","type":"movie"}]}}`)
	if err := p.Reload(ctx); err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshMarksInPlex(t *testing.T) {
	f, s := newTestService(t)
	_, p := newTestPlex(t)
	p.Reload(ctx)
	s.plex = p
	s.Create(ctx, mbid)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[{"id":1}],"statistics":{"percentOfTracks":100}}]`)
	s.Refresh(ctx, time.Now())
	r, _ := s.store.Get(mbid)
	if r.Status != "in_plex" || r.PlexKey != "123" {
		t.Fatalf("got %+v", r)
	}
}

func TestPlexFailedReloadWaits(t *testing.T) {
	p := NewPlex("http://127.0.0.1:1", "plex-token")
	if p.Reload(ctx) == nil {
		t.Fatal("reload of a dead Plex succeeded")
	}
	if p.Age() > time.Minute {
		t.Fatal("failed reload not recorded: Plex would be retried every 10 s")
	}
}

// MusicBrainz renamed Kanye West "Ye"; Plex shows the album under the name in its tags.
func TestRefreshFindsPlexAlbumUnderFormerName(t *testing.T) {
	f, s := newTestService(t)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[{"ratingKey":"789","title":"BULLY","parentTitle":"Kanye West"}]}}`)
	p.Reload(ctx)
	s.plex = p
	s.store.Put(Request{MBID: mbid, Title: "BULLY", Artist: "Ye", AlbumID: 487, Status: "imported", RequestedAt: time.Now()})
	f.reply("GET /api/v1/album", 200, `[{"id":487,"foreignAlbumId":"`+mbid+`","title":"BULLY","monitored":true,"releases":[{"id":1}],
	 "statistics":{"percentOfTracks":100},"artist":{"id":34,"foreignArtistId":"`+yeMBID+`","artistName":"Ye","monitored":true,"qualityProfileId":1}}]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "in_plex" || r.PlexKey != "789" {
		t.Fatalf("got %+v", r)
	}
}

// The user downloads an album themselves (through Prowlarr): it reaches Plex without Lidarr.
func TestRefreshFindsOwnDownloadInPlex(t *testing.T) {
	f, s := newTestService(t)
	_, p := newTestPlex(t)
	p.Reload(ctx)
	s.plex = p
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "waiting", RequestedAt: time.Now()})
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "in_plex" || r.PlexKey != "123" {
		t.Fatalf("got %+v", r)
	}
	if b := f.bodies["PUT /api/v1/album/monitor"]; len(b) != 1 || b[0] != `{"albumIds":[7],"monitored":false}` {
		t.Fatalf("Lidarr still monitors the album, it would grab a second copy: %v", b)
	}
	if f.count("GET /api/v1/release") != 0 {
		t.Fatal("searched for an album already in Plex")
	}
}

func TestRefreshInPlexAfterLidarrImportKeepsMonitoring(t *testing.T) {
	f, s := newTestService(t)
	_, p := newTestPlex(t)
	p.Reload(ctx)
	s.plex = p
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "imported", RequestedAt: time.Now()})
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[{"id":1}],"statistics":{"percentOfTracks":100}}]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "in_plex" {
		t.Fatalf("got %+v", r)
	}
	if f.count("PUT /api/v1/album/monitor") != 0 {
		t.Fatal("unmonitored an album Lidarr has: its 24-bit upgrades would stop")
	}
}

// Lidarr is halfway through its own import: Plex may show the tracks already there.
func TestRefreshPartialImportWaitsForLidarr(t *testing.T) {
	f, s := newTestService(t)
	_, p := newTestPlex(t)
	p.Reload(ctx)
	s.plex = p
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "importing", RequestedAt: time.Now()})
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[{"id":1}],"statistics":{"percentOfTracks":50}}]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status == "in_plex" || f.count("PUT /api/v1/album/monitor") != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestRefreshAlbumGoneFromLidarrButInPlex(t *testing.T) {
	f, s := newTestService(t)
	_, p := newTestPlex(t)
	p.Reload(ctx)
	s.plex = p
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "waiting", RequestedAt: time.Now()})
	f.reply("GET /api/v1/album", 200, `[]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "in_plex" || r.Note != "" {
		t.Fatalf("got %+v", r)
	}
}

// MusicBrainz names Bon Iver's second album « Bon Iver, Bon Iver »; its tags, so Plex, say « Bon Iver ».
func TestPlexFindRepeatedTitle(t *testing.T) {
	f, p := newTestPlex(t)
	f.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[
	 {"ratingKey":"7","title":"Bon Iver","parentTitle":"Bon Iver"},
	 {"ratingKey":"8","title":"22","parentTitle":"Bon Iver"}]}}`)
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if k := p.Find("Bon Iver", "Bon Iver, Bon Iver"); k != "7" {
		t.Errorf("repeated title: got %q", k)
	}
	if k := p.Find("Bon Iver", "22, A Million"); k != "" {
		t.Errorf("a comma alone must not shorten a title: got %q", k)
	}
}

func TestPlexVersion(t *testing.T) {
	f, p := newTestPlex(t)
	f.reply("GET /library/metadata/123/children", 200, `{"MediaContainer":{"Metadata":[
	 {"ratingKey":"71","title":"One More Time","index":1,"parentIndex":1,"duration":320357},
	 {"ratingKey":"72","title":"Aerodynamic","index":2,"parentIndex":1,"duration":207000}]}}`)
	f.reply("GET /library/metadata/71", 200, `{"MediaContainer":{"Metadata":[{"Media":[{"audioCodec":"flac","Part":[{"Stream":[
	 {"streamType":2,"codec":"flac","bitDepth":24,"samplingRate":96000}]}]}]}]}}`)
	v, err := p.Version(ctx, "123")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tracks) != 2 || v.Tracks[1] != (Track{Disc: 1, N: 2, Title: "Aerodynamic", Ms: 207000}) {
		t.Fatalf("tracks %+v", v.Tracks)
	}
	if v.Codec != "flac" || v.Bits != 24 || v.Rate != 96000 {
		t.Fatalf("quality %+v", v)
	}
	if _, err := p.Version(ctx, "999"); err == nil {
		t.Fatal("missing album accepted")
	}
}

const libraryJSON = `{"MediaContainer":{"Metadata":[
 {"ratingKey":"123","guid":"plex://album/5d07bcb0403c6402904a5ff5","title":"Extension of a Man","parentTitle":"Donny Hathaway","year":1973,"thumb":"/library/metadata/123/thumb/1700",
  "addedAt":1700000000,"lastViewedAt":1732000000,"viewCount":4,"leafCount":9,
  "Genre":[{"tag":"Soul"},{"tag":"R&B"}],"Guid":[{"id":"mbid://5b11f4ce-a62d-471e-81fc-a69a8278c7da"}]},
 {"ratingKey":"456","title":"Bootleg","parentTitle":"Radiohead","addedAt":1600000000}]}}`

func TestPlexLibrary(t *testing.T) {
	f, p := newTestPlex(t)
	if _, ok := p.Library(); ok {
		t.Error("library before the first load")
	}
	f.reply("GET /library/sections/3/all", 200, libraryJSON)
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if f.count("GET /library/sections/3/all?type=9&includeGuids=1") != 1 {
		t.Errorf("calls %v", f.calls)
	}
	al, ok := p.Album("123")
	if !ok || al.Cover != "/api/library/123/cover?v=1700" || al.Tracks != 9 || al.Plays != 4 || al.Played != 1732000000 ||
		al.Added != 1700000000 || al.Year != 1973 || strings.Join(al.Genres, ",") != "Soul,R&B" || al.Artist != "Donny Hathaway" {
		t.Errorf("got %+v", al)
	}
	if m := p.Mbid("123"); m != "5b11f4ce-a62d-471e-81fc-a69a8278c7da" {
		t.Errorf("mbid %q", m)
	}
	if bare, _ := p.Album("456"); bare.Cover != "" || p.Mbid("456") != "" {
		t.Errorf("got %+v", bare)
	}
	page, ok := p.Library()
	body := page.Body
	if !ok || page.Tag == "" || len(page.Gz) == 0 || strings.Contains(string(body), "plex-token") || strings.Contains(string(body), "thumb") {
		t.Fatalf("ok %v tag %q body %s", ok, page.Tag, body)
	}
	var lib struct {
		Albums []LibAlbum `json:"albums"`
		Plex   string     `json:"plex"`
	}
	if err := json.Unmarshal(body, &lib); err != nil || len(lib.Albums) != 2 || lib.Plex != "https://app.plex.tv/desktop#!/server/abc123/details?key=" {
		t.Errorf("err %v lib %+v", err, lib)
	}
	if k := p.Find("Donny Hathaway", "Extension of a Man"); k != "123" { // the index still finds albums
		t.Errorf("find %q", k)
	}
}

func TestPlexKinds(t *testing.T) {
	// Plex leaves Format and Subformat out of its album list (only an album's own page has them),
	// but it filters on them: /format and /subformat give the values, ?format=<key> their albums.
	f, p := newTestPlex(t)
	f.handle("GET /library/sections/3/all", func(w http.ResponseWriter, r *http.Request, _ string) {
		q := r.URL.Query()
		keys := map[string]string{"": "1,2,3,4,5,6,7,8", "8620": "3", "5740": "2,9", "5140": "4,2", "5157": "5", "5024": "6", "4989": "7", "5300": "8"}
		var items []string
		for _, k := range strings.Split(keys[q.Get("format")+q.Get("subformat")], ",") {
			items = append(items, `{"ratingKey":"`+k+`","guid":"plex://album/`+k+`","title":"T`+k+`","parentTitle":"Radiohead"}`)
		}
		io.WriteString(w, `{"MediaContainer":{"Metadata":[`+strings.Join(items, ",")+`]}}`)
	})
	f.reply("GET /library/sections/3/format", 200, `{"MediaContainer":{"Directory":[
	 {"key":"4581","title":"Album"},{"key":"5740","title":"EP"},{"key":"8620","title":"Single"}]}}`)
	f.reply("GET /library/sections/3/subformat", 200, `{"MediaContainer":{"Directory":[
	 {"key":"5157","title":"Compilation"},{"key":"5140","title":"Live"},{"key":"5024","title":"Remix"},{"key":"4989","title":"Soundtrack"},{"key":"5300","title":"Demo"}]}}`)
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	// 2 is a live EP: filed with the singles, as albumKind does
	want := map[string]string{"1": "", "2": "single", "3": "single", "4": "live", "5": "compilation", "6": "remix", "7": "soundtrack", "8": ""}
	for key, kind := range want {
		if al, _ := p.Album(key); al.Kind != kind {
			t.Errorf("%s: kind %q, want %q", key, al.Kind, kind)
		}
	}
	if _, ok := p.Album("9"); ok {
		t.Error("an album only seen in a filter was added")
	}
	if f.count("GET /library/sections/3/all?type=9&format=4581") != 0 {
		t.Error("the plain albums were fetched for nothing")
	}
	page, _ := p.Library()
	if !strings.Contains(string(page.Body), `"kind":"live"`) || strings.Contains(string(page.Body), `"kind":""`) {
		t.Errorf("body %s", page.Body)
	}
}

func TestPlexKindsUnavailableKeepsTheLibrary(t *testing.T) {
	f, p := newTestPlex(t) // no /format nor /subformat, as an older Plex
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if al, ok := p.Album("123"); !ok || al.Kind != "" {
		t.Errorf("got %+v %v", al, ok)
	}
	if f.count("GET /library/sections/3/subformat") != 1 {
		t.Errorf("calls %v", f.calls)
	}
}
