package main

import (
	"encoding/json"
	"testing"
	"time"
)

const pickTitle = "(Electronic) [CD] Daft Punk - Discovery - 2001, FLAC (tracks), lossless"

func waitingDiscovery(s *Service) {
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "waiting", RequestedAt: time.Now()})
}

func TestPickedStopsLidarr(t *testing.T) {
	f, s := newTestService(t)
	waitingDiscovery(s)
	now := time.Now()
	r, err := s.Picked(ctx, mbid, pickTitle, "Indexer B", now)
	if err != nil || r.Status != "picked" || r.Picked != pickTitle || r.PickedFrom != "Indexer B" || !r.PickedAt.Equal(now) {
		t.Fatalf("got %+v %v", r, err)
	}
	if got, _ := s.store.Get(mbid); got.Status != "picked" {
		t.Fatalf("stored %+v", got)
	}
	if b := f.bodies["PUT /api/v1/album/monitor"]; len(b) != 1 || b[0] != `{"albumIds":[7],"monitored":false}` {
		t.Fatalf("monitor calls %v", b)
	}
	if _, err := s.Picked(ctx, "00000000-0000-0000-0000-000000000000", pickTitle, "X", now); err != ErrNotFound {
		t.Fatalf("unknown request: %v", err)
	}
}

func TestPickedWhileLidarrDown(t *testing.T) {
	f, s := newTestService(t)
	waitingDiscovery(s)
	f.reply("PUT /api/v1/album/monitor", 500, `{"message":"down"}`)
	if r, err := s.Picked(ctx, mbid, pickTitle, "Indexer B", time.Now()); err != nil || r.Status != "picked" {
		t.Fatalf("the grab happened, the pick must be kept: %+v %v", r, err)
	}
	f.reply("PUT /api/v1/album/monitor", 202, `[]`)
	s.Refresh(ctx, time.Now())
	b := f.bodies["PUT /api/v1/album/monitor"]
	if len(b) != 2 || b[1] != `{"albumIds":[7],"monitored":false}` {
		t.Fatalf("unmonitor not retried: %v", b)
	}
}

func TestPickedIgnoresLidarrQueue(t *testing.T) {
	f, s := newTestService(t)
	waitingDiscovery(s)
	s.Picked(ctx, mbid, pickTitle, "Indexer B", time.Now())
	f.reply("GET /api/v1/queue/details", 200, `[{"albumId":7,"size":100,"sizeleft":58,"trackedDownloadState":"downloading"}]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "picked" {
		t.Fatalf("got %+v", r)
	}
	if f.count("GET /api/v1/release") != 0 {
		t.Fatal("searched again after a pick")
	}
}

// The pick lands in Plex through musictag, under the name in its tags.
func TestPickedReachesPlex(t *testing.T) {
	f, s := newTestService(t)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, `{"MediaContainer":{"Metadata":[{"ratingKey":"789","title":"BULLY","parentTitle":"Kanye West"}]}}`)
	p.Reload(ctx)
	s.plex = p
	s.store.Put(Request{MBID: mbid, Title: "BULLY", Artist: "Ye", AlbumID: 487, Status: "picked", Picked: "x", PickedAt: time.Now(), RequestedAt: time.Now()})
	f.reply("GET /api/v1/album", 200, `[{"id":487,"foreignAlbumId":"`+mbid+`","title":"BULLY","monitored":false,"releases":[{"id":1}],
	 "statistics":{"percentOfTracks":0},"artist":{"id":34,"foreignArtistId":"`+yeMBID+`","artistName":"Ye","monitored":true,"qualityProfileId":1}}]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "in_plex" || r.PlexKey != "789" || r.ArtistMBID != yeMBID {
		t.Fatalf("got %+v", r)
	}
	if f.count("PUT /api/v1/album/monitor") != 0 {
		t.Fatal("unmonitored an album Lidarr no longer monitors")
	}
}

func TestPickedTooLong(t *testing.T) {
	_, s := newTestService(t)
	now := time.Now()
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "picked", Picked: pickTitle, PickedAt: now.Add(-25 * time.Hour), RequestedAt: now})
	s.Refresh(ctx, now)
	if r, _ := s.store.Get(mbid); r.Status != "check" || r.Note != notInPlexNote {
		t.Fatalf("got %+v", r)
	}
}

func TestRetryAfterPickRemonitors(t *testing.T) {
	f, s := newTestService(t)
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "check", Picked: pickTitle, PickedFrom: "X", PickedAt: time.Now().Add(-25 * time.Hour), RequestedAt: time.Now()})
	r, err := s.Retry(ctx, mbid)
	s.searches.Wait() // Retry starts a search in the background, which calls the fake too
	if err != nil || r.Picked != "" || r.PickedFrom != "" || !r.PickedAt.IsZero() || r.Status != "searching" {
		t.Fatalf("got %+v %v", r, err)
	}
	if b := f.bodies["PUT /api/v1/album/monitor"]; len(b) != 1 || b[0] != `{"albumIds":[7],"monitored":true}` {
		t.Fatalf("monitor calls %v", b)
	}
}

func TestWaitingForPlexWhilePicked(t *testing.T) {
	st, _ := OpenStore(t.TempDir())
	st.Put(Request{MBID: mbid, Status: "picked", PickedAt: time.Now()})
	if !waitingForPlex(st, time.Now()) {
		t.Fatal("Plex not reloaded every minute while a pick waits")
	}
}

func TestSourcesAPIGrabLinksRequest(t *testing.T) {
	_, pf, a, h := newSourcesApp(t)
	waitingDiscovery(a.svc)
	items := searchSources(t, h)
	if w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`","mbid":"00000000-0000-0000-0000-000000000000"}`); w.Code != 404 || errCode(w) != "request.notFound" {
		t.Fatalf("unknown request: %d %s", w.Code, w.Body)
	}
	if pf.count("POST /api/v1/search") != 0 {
		t.Fatal("grabbed for a request that does not exist")
	}
	w := call(h, "POST", "/api/sources/grab", `{"token":"`+items[0].Token+`","mbid":"`+mbid+`"}`)
	var res struct {
		Title   string
		Request Item
	}
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Request.Status != "picked" || res.Request.Picked != items[0].Title || res.Request.PickedFrom != "Indexer B" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}
