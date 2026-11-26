package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveStopsLidarrAndForgets(t *testing.T) {
	f, s := newTestService(t)
	waitingDiscovery(s)
	if err := s.Remove(ctx, mbid); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.store.Get(mbid); ok {
		t.Fatal("still listed")
	}
	if b := f.bodies["PUT /api/v1/album/monitor"]; len(b) != 1 || b[0] != `{"albumIds":[7],"monitored":false}` {
		t.Fatalf("monitor calls %v", b)
	}
	again, err := OpenStore(filepath.Dir(s.store.path))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Get(mbid); ok {
		t.Fatal("back after a restart")
	}
	if err := s.Remove(ctx, mbid); err != ErrNotFound {
		t.Fatalf("second remove: %v", err)
	}
}

func TestRemoveKeepsRequestWhenLidarrFails(t *testing.T) {
	f, s := newTestService(t)
	waitingDiscovery(s)
	f.reply("PUT /api/v1/album/monitor", 500, `{"message":"down"}`)
	if err := s.Remove(ctx, mbid); err == nil {
		t.Fatal("no error")
	}
	if _, ok := s.store.Get(mbid); !ok {
		t.Fatal("forgotten while Lidarr still looks for the album")
	}
}

// An album Lidarr no longer has, or no longer follows, leaves nothing to stop.
func TestRemoveAlbumGoneFromLidarr(t *testing.T) {
	f, s := newTestService(t)
	waitingDiscovery(s)
	f.reply("GET /api/v1/album", 200, `[]`)
	if err := s.Remove(ctx, mbid); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.store.Get(mbid); ok || f.count("PUT /api/v1/album/monitor") != 0 {
		t.Fatalf("listed %v, monitor calls %d", ok, f.count("PUT /api/v1/album/monitor"))
	}
}

// In Plex, Lidarr may still upgrade the album: the request only leaves the list.
func TestRemoveInPlexLeavesLidarrAlone(t *testing.T) {
	f, s := newTestService(t)
	s.store.Put(Request{MBID: mbid, Title: "Discovery", AlbumID: 7, Status: "in_plex", PlexKey: "/library/metadata/1"})
	if err := s.Remove(ctx, mbid); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.store.Get(mbid); ok || f.count("PUT /api/v1/album/monitor") != 0 || f.count("GET /api/v1/album") != 0 {
		t.Fatalf("listed %v, calls %v", ok, f.calls)
	}
}

// Once a source has left, retiring the request would not stop it reaching Plex.
func TestRemoveRefusesDownloadOnItsWay(t *testing.T) {
	for _, r := range []Request{
		{Status: "downloading"}, {Status: "importing"}, {Status: "imported"},
		{Status: "picked", Picked: pickTitle, PickedAt: time.Now()},
		{Status: "searching", Source: "Daft Punk - Discovery (2001) [FLAC]"},
	} {
		f, s := newTestService(t)
		r.MBID, r.Title, r.AlbumID = mbid, "Discovery", 7
		s.store.Put(r)
		if err := s.Remove(ctx, mbid); err != ErrBusy {
			t.Fatalf("%s: %v", r.Status, err)
		}
		if _, ok := s.store.Get(mbid); !ok || f.count("PUT /api/v1/album/monitor") != 0 {
			t.Fatalf("%s: listed %v, monitor calls %d", r.Status, ok, f.count("PUT /api/v1/album/monitor"))
		}
	}
}

// Lidarr grabbed a source but holds it back (delay profile): it is on its way too.
func TestRemoveRefusesGrabHeldByLidarr(t *testing.T) {
	f, s := newTestService(t)
	s.store.Put(Request{MBID: mbid, Title: "Discovery", AlbumID: 7, Status: "searching"})
	f.reply("GET /api/v1/queue/details", 200, `[{"albumId":7,"status":"delay","trackedDownloadState":"downloading"}]`)
	if err := s.Remove(ctx, mbid); err != ErrBusy {
		t.Fatalf("got %v", err)
	}
	if f.count("PUT /api/v1/album/monitor") != 0 {
		t.Fatal("unmonitored anyway")
	}
}

// Retired while Musaarr's own search runs: its pick is never grabbed.
func TestRemoveDuringSearchGrabsNothing(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	done := hold(f, approvedJSON)
	defer done()
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "searching" {
		t.Fatalf("not searching: %+v", r)
	}
	if err := s.Remove(ctx, mbid); err != nil {
		t.Fatal(err)
	}
	done()
	s.searches.Wait()
	if n := f.count("POST /api/v1/release"); n != 0 {
		t.Fatalf("%d grabs after the request was retired", n)
	}
	if _, ok := s.store.Get(mbid); ok {
		t.Fatal("the search brought the request back")
	}
}

func TestRemoveEndpoint(t *testing.T) {
	f, a, h := newTestApp(t)
	waitingDiscovery(a.svc)
	if w := call(h, "DELETE", "/api/requests/"+mbid, ""); w.Code != http.StatusNoContent {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if w := call(h, "DELETE", "/api/requests/"+mbid, ""); w.Code != 404 || errCode(w) != "request.notFound" {
		t.Fatalf("again: %d %q", w.Code, errMsg(w))
	}
	if w := call(h, "DELETE", "/api/requests/xyz", ""); w.Code != 400 {
		t.Fatalf("bad id: %d", w.Code)
	}
	a.svc.store.Put(Request{MBID: mbid, Title: "Discovery", AlbumID: 7, Status: "downloading"})
	if w := call(h, "DELETE", "/api/requests/"+mbid, ""); w.Code != 409 || errCode(w) != "remove.busy" {
		t.Fatalf("busy: %d %q", w.Code, errMsg(w))
	}
	a.svc.store.Put(Request{MBID: mbid, Title: "Discovery", AlbumID: 7, Status: "failed"})
	f.reply("PUT /api/v1/album/monitor", 500, `{"message":"down"}`)
	if w := call(h, "DELETE", "/api/requests/"+mbid, ""); w.Code != 502 {
		t.Fatalf("Lidarr down: %d", w.Code)
	}
}

// The list says which requests can be retired, so the page offers Retirer only there.
func TestListSaysRemovable(t *testing.T) {
	_, a, h := newTestApp(t)
	waitingDiscovery(a.svc)
	a.svc.store.Put(Request{MBID: "11111111-1111-1111-1111-111111111111", Title: "Homework", Status: "downloading"})
	var out struct{ Requests []Item }
	json.Unmarshal(call(h, "GET", "/api/requests", "").Body.Bytes(), &out)
	got := map[string]bool{}
	for _, it := range out.Requests {
		got[it.Title] = it.Removable
	}
	if !got["Discovery"] || got["Homework"] || len(got) != 2 {
		t.Fatalf("removable %v", got)
	}
}
