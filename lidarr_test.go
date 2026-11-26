package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const mbid = "48117b82-a8b4-36ea-a5f0-9e8a1e8b9b4d"

const lookupJSON = `[{"id":0,"foreignAlbumId":"` + mbid + `","title":"Discovery","albumType":"Album","secondaryTypes":[],
 "releaseDate":"2001-03-12T00:00:00Z","remoteCover":"https://imagecache.lidarr.audio/v1/caa/abc/1200.jpg",
 "artist":{"foreignArtistId":"056e4f3e-d505-4dad-8ec1-d04f521cbb56","artistName":"Daft Punk"}}]`

func newTestLidarr(t *testing.T) (*fake, *Lidarr) {
	f := newFake(t)
	return f, NewLidarr(f.srv.URL, "secret-key")
}

func TestLookupMapping(t *testing.T) {
	f, l := newTestLidarr(t)
	var gotTerm string
	f.handle("GET /api/v1/album/lookup", func(w http.ResponseWriter, r *http.Request, _ string) {
		gotTerm = r.URL.Query().Get("term")
		w.Write([]byte(lookupJSON))
	})
	albums, err := l.Lookup(context.Background(), "AC/DC & Björk #1")
	if err != nil {
		t.Fatal(err)
	}
	if gotTerm != "AC/DC & Björk #1" {
		t.Errorf("term %q", gotTerm)
	}
	want := Album{MBID: mbid, Title: "Discovery", Type: "Album", Secondary: []string{}, Year: 2001,
		Cover: "https://imagecache.lidarr.audio/v1/caa/abc/1200.jpg", ArtistMBID: "056e4f3e-d505-4dad-8ec1-d04f521cbb56", ArtistName: "Daft Punk"}
	if len(albums) != 1 || !reflect.DeepEqual(albums[0], want) {
		t.Fatalf("got %+v", albums)
	}
}

func TestCoverAllowlist(t *testing.T) {
	for _, u := range []string{"https://evil.example/x.jpg", "http://imagecache.lidarr.audio/x.jpg", "javascript:alert(1)"} {
		if c := safeCover(u); c != "" {
			t.Errorf("%q allowed", u)
		}
	}
	if safeCover("https://coverartarchive.org/release/x/1.jpg") != "" {
		t.Error("coverartarchive accepted (it redirects to hosts the CSP blocks)")
	}
}

func TestAddAlbumPayload(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("POST /api/v1/album", 201, `{"id":42}`)
	a := Album{MBID: mbid, ArtistMBID: "056e4f3e-d505-4dad-8ec1-d04f521cbb56", ArtistName: "Daft Punk"}
	id, err := l.AddAlbum(context.Background(), a, 1, 3, "/data/music")
	if err != nil || id != 42 {
		t.Fatalf("id %d err %v", id, err)
	}
	var got map[string]any
	json.Unmarshal([]byte(f.bodies["POST /api/v1/album"][0]), &got)
	var want map[string]any
	json.Unmarshal([]byte(`{"foreignAlbumId":"`+mbid+`","monitored":true,"anyReleaseOk":true,
	 "addOptions":{"searchForNewAlbum":false},
	 "artist":{"foreignArtistId":"056e4f3e-d505-4dad-8ec1-d04f521cbb56","artistName":"Daft Punk",
	  "qualityProfileId":1,"metadataProfileId":3,"rootFolderPath":"/data/music","monitored":true,
	  "monitorNewItems":"none","tags":[],
	  "addOptions":{"monitor":"none","albumsToMonitor":["`+mbid+`"],"searchForMissingAlbums":false}}}`), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload\n got %v\nwant %v", got, want)
	}
}

func TestAddAlbumAlreadyAdded(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("POST /api/v1/album", 400, `[{"propertyName":"ForeignAlbumId","errorMessage":"This album has already been added."}]`)
	_, err := l.AddAlbum(context.Background(), Album{MBID: mbid}, 1, 3, "/data/music")
	if !errors.Is(err, ErrAlreadyAdded) {
		t.Fatalf("got %v", err)
	}
}

func TestLidarrErrorHasNoKey(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("POST /api/v1/album", 400, `[{"errorMessage":"Album has no releases"}]`)
	_, err := l.AddAlbum(context.Background(), Album{MBID: mbid}, 1, 3, "/data/music")
	var le *LidarrError
	if !errors.As(err, &le) || le.Message != "Album has no releases" || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("got %v", err)
	}
}

func TestAlbumByMBID(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","title":"Discovery","monitored":true,
	 "releases":[{"id":1},{"id":2}],"statistics":{"percentOfTracks":50},
	 "artist":{"id":3,"monitored":false,"qualityProfileId":2,"artistName":"Daft Punk"}}]`)
	a, err := l.AlbumByMBID(context.Background(), mbid)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	if a.ID != 7 || !a.Monitored || a.Releases != 2 || a.Percent != 50 || a.ArtistID != 3 || a.ArtistMonitored || a.ArtistQualityProfileID != 2 {
		t.Fatalf("got %+v", a)
	}
	f.reply("GET /api/v1/album", 200, `[]`)
	if a, err := l.AlbumByMBID(context.Background(), mbid); a != nil || err != nil {
		t.Fatalf("empty: %v %v", a, err)
	}
}

func TestMonitor(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("PUT /api/v1/album/monitor", 202, `[]`)
	if err := l.MonitorAlbum(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies["PUT /api/v1/album/monitor"][0]; b != `{"albumIds":[7],"monitored":true}` {
		t.Errorf("monitor body %s", b)
	}
}

// Field names and values as Lidarr 3.1.6 answered on a real instance (22/11/2026).
func TestReleasesAndGrab(t *testing.T) {
	f, l := newTestLidarr(t)
	var q string
	f.handle("GET /api/v1/release", func(w http.ResponseWriter, r *http.Request, _ string) {
		q = r.URL.RawQuery
		w.Write([]byte(`[{"guid":"g1","indexerId":2,"indexer":"Indexer A (Prowlarr)","title":"Kanye West - 2005 - Late Registration [FLAC - 44.1 kHz / 16 bits]",
		 "protocol":"torrent","seeders":7,"size":443000000,"approved":false,"rejected":true,"downloadAllowed":true,"releaseWeight":0,
		 "rejections":["Wrong album"],"quality":{"quality":{"id":6,"name":"FLAC"}},"downloadUrl":"https://tracker.example/passkey"}]`))
	})
	rels, err := l.Releases(context.Background(), 488)
	if err != nil || q != "albumId=488" || len(rels) != 1 {
		t.Fatalf("got %+v %v query %q", rels, err, q)
	}
	r := rels[0]
	if r.GUID != "g1" || r.IndexerID != 2 || r.Seeders == nil || *r.Seeders != 7 || !r.Rejected || !r.DownloadAllowed ||
		r.Rejections[0] != "Wrong album" || r.Quality.Quality.ID != 6 || r.Quality.Quality.Name != "FLAC" {
		t.Fatalf("mapping %+v", r)
	}
	f.reply("POST /api/v1/release", 200, `{}`)
	if err := l.Grab(context.Background(), r, 488, 34); err != nil {
		t.Fatal(err)
	}
	if b := f.bodies["POST /api/v1/release"][0]; b != `{"guid":"g1","indexerId":2,"albumId":488,"artistId":34}` {
		t.Errorf("grab body %s", b)
	}
}

func TestCommandGone(t *testing.T) {
	_, l := newTestLidarr(t)
	c, err := l.Command(context.Background(), 5)
	if err != nil || !c.Gone {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestCommandMapping(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("GET /api/v1/command/5", 200, `{"id":5,"status":"completed","message":"Album search completed. 0 reports downloaded.","ended":"2026-11-21T20:00:00Z"}`)
	c, err := l.Command(context.Background(), 5)
	if err != nil || c.Status != "completed" || !strings.Contains(c.Message, "0 reports") || c.Ended.IsZero() {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestQueueMapping(t *testing.T) {
	f, l := newTestLidarr(t)
	var q string
	f.handle("GET /api/v1/queue/details", func(w http.ResponseWriter, r *http.Request, _ string) {
		q = r.URL.RawQuery
		w.Write([]byte(`[{"albumId":7,"size":1000,"sizeleft":250,"status":"downloading","trackedDownloadState":"downloading","timeleft":"00:03:00","downloadId":"ABC"}]`))
	})
	items, err := l.Queue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := QueueItem{AlbumID: 7, Size: 1000, Sizeleft: 250, Status: "downloading", State: "downloading", Timeleft: "00:03:00", DownloadID: "ABC"}
	if len(items) != 1 || items[0] != want || q != "" { // the whole queue: a download Lidarr could not map has no albumId
		t.Fatalf("got %+v query %q", items, q)
	}
}

func TestProfileIDs(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("GET /api/v1/qualityprofile", 200, `[{"id":1,"name":"Any","items":[{"quality":{"id":4},"items":[],"allowed":true}]},
	 {"id":4,"name":"Hi-Fi","items":[{"quality":{"id":0,"name":"Unknown"},"items":[],"allowed":false},
	  {"quality":{"id":4,"name":"MP3-320"},"items":[],"allowed":false},
	  {"id":1005,"name":"Lossless","allowed":true,"items":[{"quality":{"id":6,"name":"FLAC"},"items":[],"allowed":true},{"quality":{"id":7,"name":"ALAC"},"items":[],"allowed":true}]},
	  {"id":1006,"name":"Lossless 24 bits","allowed":true,"items":[{"quality":{"id":21,"name":"FLAC 24bit"},"items":[],"allowed":true}]}]}]`)
	f.reply("GET /api/v1/metadataprofile", 200, `[{"id":2,"name":"Standard"},{"id":5,"name":"None"}]`)
	f.reply("GET /api/v1/rootfolder", 200, `[{"id":1,"path":"/data/music"}]`)
	p, err := l.Profiles(context.Background(), "Hi-Fi", "None", "/data/music")
	if err != nil || p.QualityID != 4 || p.MetadataID != 5 || p.QualityNames[1] != "Any" {
		t.Fatalf("got %+v %v", p, err)
	}
	if want := map[int]bool{6: true, 7: true, 21: true}; !reflect.DeepEqual(p.Allowed, want) {
		t.Fatalf("allowed qualities %v, want %v", p.Allowed, want)
	}
	_, err = l.Profiles(context.Background(), "Lossless", "None", "/data/music")
	if err == nil || !strings.Contains(err.Error(), "Lossless") {
		t.Fatalf("missing quality: %v", err)
	}
	_, err = l.Profiles(context.Background(), "Hi-Fi", "None", "/music")
	if err == nil || !strings.Contains(err.Error(), "/music") {
		t.Fatalf("missing root: %v", err)
	}
}

func TestLidarrCoverHost(t *testing.T) {
	u := "https://images.lidarr.audio/cache/https://coverartarchive.org/release/x/1-1200.jpg"
	if safeCover(u) != u {
		t.Fatal("images.lidarr.audio rejected: every cover was dropped")
	}
}

func TestArtistLookup(t *testing.T) {
	f, l := newTestLidarr(t)
	f.reply("GET /api/v1/artist/lookup", 200, `[{"artistName":"Ye","foreignArtistId":"164f0d73-1234-4e2c-8743-d77bf2191051",
	 "disambiguation":"formerly Kanye West","remotePoster":"https://images.lidarr.audio/p.jpg"},
	 {"artistName":"Nobody","foreignArtistId":"x","images":[{"coverType":"poster","remoteUrl":"https://evil.example/p.jpg"}]}]`)
	got, err := l.ArtistLookup(context.Background(), "kanye west")
	if err != nil {
		t.Fatal(err)
	}
	want := Artist{MBID: "164f0d73-1234-4e2c-8743-d77bf2191051", Name: "Ye", Disambiguation: "formerly Kanye West", Image: "https://images.lidarr.audio/p.jpg"}
	if len(got) != 2 || got[0] != want || got[1].Image != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestGrabbedDownloadID(t *testing.T) {
	f, l := newTestLidarr(t)
	var q url.Values
	f.handle("GET /api/v1/history", func(w http.ResponseWriter, r *http.Request, _ string) {
		q = r.URL.Query()
		w.Write([]byte(`{"page":1,"records":[{"eventType":"trackFileImported","downloadId":"OLD"},{"eventType":"grabbed","downloadId":"ABC"},{"eventType":"grabbed","downloadId":"OLDER"}]}`))
	})
	id, err := l.GrabbedDownloadID(context.Background(), 481)
	if err != nil || id != "ABC" || q.Get("albumId") != "481" || q.Get("sortDirection") != "descending" {
		t.Fatalf("got %q %v query %v", id, err, q)
	}
	f.reply("GET /api/v1/history", 200, `{"records":[]}`)
	if id, err := l.GrabbedDownloadID(context.Background(), 481); id != "" || err != nil {
		t.Fatalf("no grab: %q %v", id, err)
	}
}

// The shape Lidarr 3.1.6 answered and accepted for Yeezus on a real instance (22/11/2026).
func TestManualImport(t *testing.T) {
	f, l := newTestLidarr(t)
	var q url.Values
	f.handle("GET /api/v1/manualimport", func(w http.ResponseWriter, r *http.Request, _ string) {
		q = r.URL.Query()
		w.Write([]byte(`[{"id":1,"path":"/data/torrents/music/Kanye West - Yeezus/01 - On Sight.flac","name":"01 - On Sight",
		 "artist":{"id":34},"album":{"id":481,"title":"Yeezus"},"albumReleaseId":6254,"tracks":[{"id":101448}],
		 "quality":{"quality":{"id":6,"name":"FLAC"},"revision":{"version":1}},"indexerFlags":0,"downloadId":"ABC","rejections":[],"additionalFile":false}]`))
	})
	items, err := l.ImportItems(context.Background(), "ABC", 34)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	if q.Get("downloadId") != "ABC" || q.Get("artistId") != "34" || q.Get("filterExistingFiles") != "true" {
		t.Fatalf("query %v", q)
	}
	it := items[0]
	if it.Album.ID != 481 || it.AlbumReleaseID != 6254 || len(it.Tracks) != 1 || it.Tracks[0].ID != 101448 || len(it.Rejections) != 0 {
		t.Fatalf("mapping %+v", it)
	}
	f.reply("POST /api/v1/command", 201, `{"id":1065,"status":"started"}`)
	id, err := l.ManualImport(context.Background(), items, 34, 481, "ABC")
	if err != nil || id != 1065 {
		t.Fatal(id, err)
	}
	want := `{"name":"ManualImport","importMode":"auto","replaceExistingFiles":false,"files":[{"path":"/data/torrents/music/Kanye West - Yeezus/01 - On Sight.flac",` +
		`"artistId":34,"albumId":481,"albumReleaseId":6254,"trackIds":[101448],"quality":{"quality":{"id":6,"name":"FLAC"},"revision":{"version":1}},` +
		`"indexerFlags":0,"downloadId":"ABC","disableReleaseSwitching":false}]}`
	if b := f.bodies["POST /api/v1/command"][0]; b != want {
		t.Errorf("command body\n%s\nwant\n%s", b, want)
	}
}
