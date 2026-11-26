package main

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const approvedJSON = `[{"guid":"g1","indexerId":1,"title":"Daft Punk - Discovery (2001) [FLAC]","protocol":"torrent","seeders":4,
 "approved":true,"rejected":false,"downloadAllowed":true,"rejections":[],"quality":{"quality":{"id":6,"name":"FLAC"}}}]`

// hold makes Lidarr's interactive search wait until the returned func is called.
func hold(f *fake, body string) (release func()) {
	ch := make(chan struct{})
	f.handle("GET /api/v1/release", func(w http.ResponseWriter, r *http.Request, _ string) {
		<-ch
		w.Write([]byte(body))
	})
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

func TestRefreshSearchesOnceWhenReady(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[]}]`)
	s.Create(ctx, mbid)
	now := time.Now()
	s.Refresh(ctx, now)
	s.searches.Wait()
	if f.count("GET /api/v1/release") != 0 {
		t.Fatal("searched before releases exist")
	}
	f.reply("GET /api/v1/album", 200, albumJSON)
	done := hold(f, approvedJSON)
	defer done()
	s.Refresh(ctx, now)
	if r, _ := s.store.Get(mbid); r.Status != "searching" {
		t.Fatalf("during the search: %+v", r)
	}
	s.Refresh(ctx, now)
	done()
	s.searches.Wait()
	s.Refresh(ctx, now)
	if n := f.count("GET /api/v1/release"); n != 1 {
		t.Fatalf("%d searches", n)
	}
	if b := f.bodies["POST /api/v1/release"]; len(b) != 1 || b[0] != `{"guid":"g1","indexerId":1,"albumId":7,"artistId":3}` {
		t.Fatalf("grabs %v", b)
	}
	if r, _ := s.store.Get(mbid); r.Status != "searching" { // grabbed, not in the queue yet
		t.Fatalf("after the grab: %+v", r)
	}
}

// Late Registration on a real Lidarr: both FLAC sources refused because the artist is now "Ye".
func TestSearchForcesSourceUnderFormerName(t *testing.T) {
	f, s := newTestService(t)
	s.store.Put(Request{MBID: mbid, Title: "Late Registration", Artist: "Ye", AlbumID: 488, Status: "requested", RequestedAt: time.Now()})
	f.reply("GET /api/v1/album", 200, `[{"id":488,"foreignAlbumId":"`+mbid+`","title":"Late Registration","monitored":true,"releases":[{"id":1}],
	 "artist":{"id":34,"foreignArtistId":"`+yeMBID+`","artistName":"Ye","monitored":true,"qualityProfileId":1}}]`)
	f.reply("GET /api/v1/release", 200, `[
	 {"guid":"dots","indexerId":2,"title":"Kanye.West.Late.Registration.2005.CD.FLAC.16Bit.44.1Khz-BEN1","protocol":"torrent","seeders":3,
	  "rejected":true,"downloadAllowed":false,"releaseWeight":1,"rejections":["Unable to parse release"],"quality":{"quality":{"id":6,"name":"FLAC"}}},
	 {"guid":"dash","indexerId":2,"title":"Kanye West - 2005 - Late Registration [FLAC - 44.1 kHz / 16 bits]","protocol":"torrent","seeders":7,
	  "rejected":true,"downloadAllowed":true,"releaseWeight":0,"rejections":["Wrong album"],"quality":{"quality":{"id":6,"name":"FLAC"}}}]`)
	s.Refresh(ctx, time.Now())
	s.searches.Wait()
	if b := f.bodies["POST /api/v1/release"]; len(b) != 1 || b[0] != `{"guid":"dash","indexerId":2,"albumId":488,"artistId":34}` {
		t.Fatalf("grabs %v", b)
	}
}

func TestSearchWithoutSourceWaits(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	f.reply("GET /api/v1/release", 200, `[{"guid":"g1","indexerId":1,"title":"Daft Punk - Discovery (2001) [MP3 320]","protocol":"torrent","seeders":4,
	 "rejected":true,"rejections":["MP3-320 is not wanted in profile"],"quality":{"quality":{"id":4,"name":"MP3-320"}}}]`)
	now := time.Now()
	s.Refresh(ctx, now)
	s.searches.Wait()
	s.Refresh(ctx, now)
	if f.count("POST /api/v1/release") != 0 {
		t.Fatal("grabbed a lossy source")
	}
	if r, _ := s.store.Get(mbid); r.Status != "waiting" {
		t.Fatalf("got %+v", r)
	}
	s.Refresh(ctx, now.Add(time.Hour))
	s.searches.Wait()
	if n := f.count("GET /api/v1/release"); n != 1 {
		t.Fatalf("searched %d times on its own", n)
	}
}

func TestSearchErrorWaitsWithoutResending(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	f.reply("GET /api/v1/release", 500, `{"message":"boom"}`)
	now := time.Now()
	for range 3 {
		s.Refresh(ctx, now)
		s.searches.Wait()
	}
	if n := f.count("GET /api/v1/release"); n != 1 {
		t.Fatalf("%d searches sent", n)
	}
	if r, _ := s.store.Get(mbid); r.Status != "waiting" {
		t.Fatalf("got %+v", r)
	}
}

// A search cut short by a restart must not show "searching" forever.
func TestSearchInterruptedByRestart(t *testing.T) {
	f, s := newTestService(t)
	now := time.Now()
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "searching",
		RequestedAt: now.Add(-time.Hour), SearchedAt: now.Add(-10 * time.Minute), Cmd: &Command{Status: "started"}})
	s.Refresh(ctx, now)
	s.searches.Wait()
	if f.count("GET /api/v1/release") != 0 {
		t.Fatal("searched again on its own")
	}
	if r, _ := s.store.Get(mbid); r.Status != "waiting" {
		t.Fatalf("got %+v", r)
	}
}

func TestRefreshDownloadingThenImported(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	f.reply("GET /api/v1/release", 200, approvedJSON)
	f.reply("GET /api/v1/queue/details", 200, `[{"albumId":7,"size":100,"sizeleft":58,"trackedDownloadState":"downloading","timeleft":"00:01:00"}]`)
	now := time.Now()
	s.Refresh(ctx, now)
	s.searches.Wait()
	s.Refresh(ctx, now)
	if r, _ := s.store.Get(mbid); r.Status != "downloading" || r.Progress != 42 {
		t.Fatalf("got %+v", r)
	}
	f.reply("GET /api/v1/queue/details", 200, `[]`)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[{"id":1}],"statistics":{"percentOfTracks":100}}]`)
	s.Refresh(ctx, now)
	if r, _ := s.store.Get(mbid); r.Status != "imported" {
		t.Fatalf("got %+v", r)
	}
}

// Requests searched by Lidarr's AlbumSearch before this version keep reading their command.
func TestRefreshReadsOldCommand(t *testing.T) {
	f, s := newTestService(t)
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "searching", RequestedAt: time.Now(), CommandID: 99})
	f.reply("GET /api/v1/command/99", 200, `{"status":"completed","message":"Album search completed. 0 reports downloaded."}`)
	s.Refresh(ctx, time.Now())
	s.searches.Wait()
	if f.count("GET /api/v1/release") != 0 {
		t.Fatal("searched again")
	}
	if r, _ := s.store.Get(mbid); r.Status != "waiting" {
		t.Fatalf("got %+v", r)
	}
}

func TestRetry(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	s.Refresh(ctx, time.Now())
	s.searches.Wait()
	done := hold(f, approvedJSON)
	defer done()
	r, err := s.Retry(ctx, mbid)
	if err != nil || r.Status != "searching" {
		t.Fatalf("got %+v %v", r, err)
	}
	if _, err := s.Retry(ctx, mbid); err != nil { // a second tap while it runs sends nothing
		t.Fatal(err)
	}
	done()
	s.searches.Wait()
	if n := f.count("GET /api/v1/release"); n != 2 {
		t.Fatalf("%d searches", n)
	}
	if f.count("POST /api/v1/release") != 1 {
		t.Fatal("retry did not grab")
	}
	if _, err := s.Retry(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

// Yeezus on a real Lidarr: the grab of "Kanye.West.Yeezus.2013.WEB.FLAC.Lossless-EXTRAP" downloaded,
// then stopped at "Unable to parse download" with no album in the queue.
func blockedYeezus(t *testing.T, items string) (*fake, *Service, time.Time) {
	f, s := newTestService(t)
	now := time.Now()
	s.store.Put(Request{MBID: mbid, Title: "Yeezus", Artist: "Ye", AlbumID: 481, Status: "searching", RequestedAt: now,
		SearchedAt: now, Source: "Kanye.West.Yeezus.2013.WEB.FLAC.Lossless-EXTRAP",
		Cmd: &Command{Status: "completed", Message: "1 reports downloaded.", Ended: now}})
	f.reply("GET /api/v1/album", 200, `[{"id":481,"foreignAlbumId":"`+mbid+`","title":"Yeezus","monitored":true,"releases":[{"id":1}],
	 "artist":{"id":34,"foreignArtistId":"`+yeMBID+`","artistName":"Ye","monitored":true,"qualityProfileId":1}}]`)
	f.reply("GET /api/v1/history", 200, `{"records":[{"eventType":"grabbed","downloadId":"ABC"}]}`)
	f.reply("GET /api/v1/queue/details", 200, `[{"albumId":null,"downloadId":"ABC","status":"completed","trackedDownloadState":"importBlocked","size":100,"sizeleft":0}]`)
	f.reply("GET /api/v1/manualimport", 200, items)
	f.reply("POST /api/v1/command", 201, `{"id":1065}`)
	f.reply("GET /api/v1/command/1065", 200, `{"status":"started"}`)
	return f, s, now
}

func TestUnreadableDownloadIsImported(t *testing.T) {
	f, s, now := blockedYeezus(t, `[
	 {"path":"/d/01.flac","album":{"id":481},"albumReleaseId":6254,"tracks":[{"id":1}],"quality":{"quality":{"id":6}},"rejections":[]},
	 {"path":"/d/02.flac","album":{"id":481},"albumReleaseId":6254,"tracks":[{"id":2}],"quality":{"quality":{"id":6}},"rejections":[]},
	 {"path":"/d/cover.jpg","additionalFile":true,"album":null,"tracks":[]}]`)
	s.Refresh(ctx, now)
	s.Refresh(ctx, now)
	if b := f.bodies["POST /api/v1/command"]; len(b) != 1 || !strings.Contains(b[0], `"albumId":481`) || strings.Contains(b[0], "cover.jpg") {
		t.Fatalf("imports %v", b)
	}
	if r, _ := s.store.Get(mbid); r.Status != "importing" || r.DownloadID != "ABC" {
		t.Fatalf("got %+v", r)
	}
	f.reply("GET /api/v1/queue/details", 200, `[]`)
	f.reply("GET /api/v1/album", 200, `[{"id":481,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[{"id":1}],"statistics":{"percentOfTracks":100}}]`)
	s.Refresh(ctx, now)
	if r, _ := s.store.Get(mbid); r.Status != "imported" {
		t.Fatalf("got %+v", r)
	}
}

func TestUnreadableDownloadWithStrangeFilesWaitsForTheUser(t *testing.T) {
	for name, items := range map[string]string{
		"other album": `[{"path":"/d/01.flac","album":{"id":999},"tracks":[{"id":1}],"rejections":[]}]`,
		"no track":    `[{"path":"/d/01.flac","album":{"id":481},"tracks":[],"rejections":[]}]`,
		"rejected":    `[{"path":"/d/01.flac","album":{"id":481},"tracks":[{"id":1}],"rejections":[{"reason":"Not an upgrade","type":"permanent"}]}]`,
		"same track":  `[{"path":"/d/01.flac","album":{"id":481},"tracks":[{"id":1}]},{"path":"/d/01b.flac","album":{"id":481},"tracks":[{"id":1}]}]`,
		"nothing":     `[]`,
	} {
		f, s, now := blockedYeezus(t, items)
		s.Refresh(ctx, now)
		s.Refresh(ctx, now.Add(time.Hour))
		if f.count("POST /api/v1/command") != 0 {
			t.Errorf("%s: imported", name)
		}
		if r, _ := s.store.Get(mbid); r.Status != "check" {
			t.Errorf("%s: got %+v", name, r)
		}
	}
}
