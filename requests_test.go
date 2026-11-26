package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const albumJSON = `[{"id":7,"foreignAlbumId":"` + mbid + `","title":"Discovery","monitored":true,"releases":[{"id":1}],
 "statistics":{"percentOfTracks":0},"artist":{"id":3,"monitored":true,"qualityProfileId":1,"artistName":"Daft Punk"}}]`

func newTestService(t *testing.T) (*fake, *Service) {
	f, l := newTestLidarr(t)
	st, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.reply("GET /api/v1/album/lookup", 200, lookupJSON)
	f.reply("POST /api/v1/album", 201, `{"id":7}`)
	f.reply("GET /api/v1/album", 200, albumJSON)
	f.reply("PUT /api/v1/album/monitor", 202, `[]`)
	f.reply("GET /api/v1/release", 200, `[]`)
	f.reply("POST /api/v1/release", 200, `{}`)
	f.reply("GET /api/v1/queue/details", 200, `[]`)
	f.reply("GET /api/v1/artist", 200, `[{"id":3,"foreignArtistId":"`+artistMBID+`"}]`)
	f.reply("PUT /api/v1/artist/editor", 202, `[]`)
	s := &Service{lidarr: l, store: st, meta: newTestMetadata(t), root: "/data/music",
		prof: Profiles{QualityID: 1, MetadataID: 5, QualityNames: map[int]string{1: "Hi-Fi", 2: "Standard"}, Allowed: hifi}}
	t.Cleanup(s.searches.Wait) // before the store's TempDir is removed
	return f, s
}

var ctx = context.Background()

const artistMBID = "056e4f3e-d505-4dad-8ec1-d04f521cbb56"

func TestCreateNewArtistGetsMonitored(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/artist", 200, `[{"id":9,"foreignArtistId":"someone-else"}]`)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","artist":{"id":3,"monitored":false,"qualityProfileId":1}}]`)
	r, _, err := s.Create(ctx, mbid)
	if err != nil {
		t.Fatal(err)
	}
	if b := f.bodies["PUT /api/v1/artist/editor"]; len(b) != 1 || b[0] != `{"artistIds":[3],"monitored":true}` {
		t.Fatalf("editor calls %v", b)
	}
	if r.Note != "" {
		t.Fatalf("note %q", r.Note)
	}
}

func TestCreateExistingArtistUntouched(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","artist":{"id":3,"monitored":false,"qualityProfileId":1}}]`)
	r, _, _ := s.Create(ctx, mbid)
	if f.count("PUT /api/v1/artist/editor") != 0 {
		t.Fatal("existing artist modified")
	}
	if r.Note != unmonitoredNote {
		t.Fatalf("note %q", r.Note)
	}
}

func TestCreateNewAlbum(t *testing.T) {
	f, s := newTestService(t)
	r, created, err := s.Create(ctx, mbid)
	if err != nil || !created {
		t.Fatal(err, created)
	}
	if f.count("POST /api/v1/album") != 1 || f.count("GET /api/v1/release") != 0 {
		t.Fatalf("calls %v", f.calls)
	}
	if r.Status != "requested" || r.AlbumID != 7 || r.Title != "Discovery" || r.Artist != "Daft Punk" || r.Note != "" {
		t.Fatalf("got %+v", r)
	}
	if _, ok := s.store.Get(mbid); !ok {
		t.Fatal("not stored")
	}
	if _, created, _ := s.Create(ctx, mbid); created || f.count("POST /api/v1/album") != 1 {
		t.Fatal("second create not idempotent")
	}
}

func TestCreateAlreadyInLidarr(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/album/lookup", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","title":"Discovery","artist":{"artistName":"Daft Punk"}}]`)
	if _, _, err := s.Create(ctx, mbid); err != nil {
		t.Fatal(err)
	}
	if f.count("POST /api/v1/album") != 0 || f.bodies["PUT /api/v1/album/monitor"][0] != `{"albumIds":[7],"monitored":true}` {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestCreateAlreadyAddedFallback(t *testing.T) {
	f, s := newTestService(t)
	f.reply("POST /api/v1/album", 400, `[{"errorMessage":"This album has already been added."}]`)
	if _, _, err := s.Create(ctx, mbid); err != nil {
		t.Fatal(err)
	}
	if f.count("PUT /api/v1/album/monitor") != 1 {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestCreateConcurrentSameMBID(t *testing.T) {
	f, s := newTestService(t)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); s.Create(ctx, mbid) }()
	}
	wg.Wait()
	if n := f.count("POST /api/v1/album"); n != 1 {
		t.Fatalf("%d POSTs", n)
	}
}

func TestCreateLidarrRejects(t *testing.T) {
	f, s := newTestService(t)
	f.reply("POST /api/v1/album", 400, `[{"errorMessage":"Album has no releases"}]`)
	_, _, err := s.Create(ctx, mbid)
	var le *LidarrError
	if !errors.As(err, &le) {
		t.Fatalf("got %v", err)
	}
	if _, ok := s.store.Get(mbid); ok {
		t.Fatal("stored despite error")
	}
}

func TestCreateNotFound(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/album/lookup", 200, `[]`)
	if _, _, err := s.Create(ctx, mbid); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestCreateExistingArtistNote(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","artist":{"id":3,"monitored":true,"qualityProfileId":2}}]`)
	r, _, _ := s.Create(ctx, mbid)
	if r.Note != profileNote+":Standard" {
		t.Fatalf("note %q", r.Note)
	}
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","artist":{"id":3,"monitored":false,"qualityProfileId":1}}]`)
	s.store, _ = OpenStore(t.TempDir())
	r, _, _ = s.Create(ctx, mbid)
	if r.Note != unmonitoredNote {
		t.Fatalf("note %q", r.Note)
	}
}

// The artist's settings only matter until a source is found: an album on its way or in Plex
// no longer says « Artiste non suivi ».
func TestArtistNoteOnlyWhileNoSource(t *testing.T) {
	f, s := newTestService(t)
	unmonitored := func(percent int) string {
		return `[{"id":7,"foreignAlbumId":"` + mbid + `","title":"Discovery","monitored":true,"releases":[{"id":1}],
		 "statistics":{"percentOfTracks":` + fmt.Sprint(percent) + `},"artist":{"id":3,"monitored":false,"qualityProfileId":1,"artistName":"Daft Punk"}}]`
	}
	f.reply("GET /api/v1/album", 200, unmonitored(0))
	s.store.Put(Request{MBID: mbid, Title: "Discovery", Artist: "Daft Punk", AlbumID: 7, Status: "waiting", RequestedAt: time.Now(),
		SearchedAt: time.Now(), Cmd: &Command{Status: "completed", Message: "Recherche terminée, 0 reports downloaded.", Ended: time.Now()}})
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Status != "waiting" || r.Note != unmonitoredNote {
		t.Fatalf("waiting: %+v", r)
	}
	f.reply("GET /api/v1/album", 200, unmonitored(100))
	s.Refresh(ctx, time.Now().Add(time.Hour))
	if r, _ := s.store.Get(mbid); r.Status != "imported" || r.Note != "" {
		t.Fatalf("imported: %+v", r)
	}
}

func TestRefreshSkipsNotDue(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	r, _ := s.store.Get(mbid)
	r.Status, r.CheckedAt = "waiting", time.Now()
	s.store.Put(r)
	before := f.count("GET /api/v1/album?")
	s.Refresh(ctx, time.Now().Add(time.Minute))
	if f.count("GET /api/v1/album?") != before {
		t.Fatal("waiting request refreshed before 15 min")
	}
	s.Refresh(ctx, time.Now().Add(16*time.Minute))
	if f.count("GET /api/v1/album?") == before {
		t.Fatal("waiting request never refreshed")
	}
}

// An album in Plex is never read again, so a note written on it before v3.1.1 (« Artiste non suivi :
// seule l'appli cherchera cet album ») would stay for good: the store drops it on load.
func TestStoreDropsNotesOfAlbumsInPlex(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "requests.json"), []byte(`[
	 {"mbid":"a","status":"in_plex","note":"Artiste non suivi : seule l'appli cherchera cet album"},
	 {"mbid":"b","status":"waiting","note":"Artiste non suivi : seule l'appli cherchera cet album"}]`), 0o600)
	st, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := st.Get("a"); a.Note != "" {
		t.Errorf("in Plex: %q", a.Note)
	}
	if b, _ := st.Get("b"); b.Note == "" {
		t.Error("a waiting request lost its note: its next pass rewrites it")
	}
}

func TestStoreReload(t *testing.T) {
	dir := t.TempDir()
	st, _ := OpenStore(dir)
	st.Put(Request{MBID: "a", RequestedAt: time.Unix(1, 0)})
	st.Put(Request{MBID: "b", RequestedAt: time.Unix(2, 0)})
	st2, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := st2.All()
	if len(all) != 2 || all[0].MBID != "b" {
		t.Fatalf("got %+v", all)
	}
	path := filepath.Join(dir, "requests.json")
	os.WriteFile(path, []byte("{broken"), 0o600)
	if _, err := OpenStore(dir); err == nil {
		t.Fatal("corrupt file accepted")
	}
	if b, _ := os.ReadFile(path); string(b) != "{broken" {
		t.Fatal("corrupt file overwritten")
	}
}

func TestStoreDiskFailureKeepsMemory(t *testing.T) {
	dir := t.TempDir()
	st, _ := OpenStore(dir)
	os.RemoveAll(dir)
	if err := st.Put(Request{MBID: "a", CommandID: 99}); !errors.Is(err, ErrStore) {
		t.Fatalf("got %v", err)
	}
	if r, ok := st.Get("a"); !ok || r.CommandID != 99 {
		t.Fatal("update lost from memory")
	}
}

func TestRequestedTooLongBecomesCheck(t *testing.T) {
	f, s := newTestService(t)
	f.reply("GET /api/v1/album", 200, `[{"id":7,"foreignAlbumId":"`+mbid+`","monitored":true,"releases":[]}]`)
	s.Create(ctx, mbid)
	path := s.store.path
	before, _ := os.ReadFile(path)
	s.Refresh(ctx, time.Now())
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("requests.json rewritten although nothing changed")
	}
	s.Refresh(ctx, time.Now().Add(notReadyAfter))
	r, _ := s.store.Get(mbid)
	if r.Status != "check" || r.Note == "" {
		t.Fatalf("got %+v", r)
	}
}

func TestAlbumBackClearsNote(t *testing.T) {
	f, s := newTestService(t)
	s.Create(ctx, mbid)
	f.reply("GET /api/v1/album", 200, `[]`)
	s.Refresh(ctx, time.Now())
	if r, _ := s.store.Get(mbid); r.Note != removedNote {
		t.Fatalf("note %q", r.Note)
	}
	f.reply("GET /api/v1/album", 200, albumJSON)
	s.Refresh(ctx, time.Now().Add(16*time.Minute))
	if r, _ := s.store.Get(mbid); r.Note != "" || r.Status == "check" {
		t.Fatalf("got %+v", r)
	}
}

func TestImportedSlowsDownAfterPlexWait(t *testing.T) {
	now := time.Now()
	r := Request{Status: "imported", ImportedAt: now, CheckedAt: now}
	if !due(r, now.Add(time.Minute)) {
		t.Fatal("fresh import not re-checked every minute")
	}
	r.ImportedAt = now.Add(-plexWait)
	if due(r, now.Add(time.Minute)) {
		t.Fatal("old import still checked every minute")
	}
	st, _ := OpenStore(t.TempDir())
	st.Put(r)
	if waitingForPlex(st, now) {
		t.Fatal("old import keeps Plex reloading every minute")
	}
}
