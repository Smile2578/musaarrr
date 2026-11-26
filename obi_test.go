package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestJapanReleases(t *testing.T) {
	d, err := newAlbumMetadata(t, lateRegJSON).Album(ctx, lateRegMBID)
	if err != nil {
		t.Fatal(err)
	}
	if got := japanReleases(d); !slices.Equal(got, []string{"r-jp"}) {
		t.Fatalf("got %q", got)
	}
}

// fakeArchive is the Cover Art Archive: a release's image list, then the images.
func fakeArchive(t *testing.T) *atomic.Int32 {
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release/r-jp":
			lists.Add(1)
			w.Write([]byte(`{"images":[{"front":false,"id":1,"types":["Back"]},{"front":true,"id":123456,"types":["Front"]}]}`))
		case "/release/r-jp/123456-250.jpg", "/release/r-jp/123456-500.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("OBI"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := coverArtBase
	coverArtBase = srv.URL
	t.Cleanup(func() { coverArtBase = old })
	return &lists
}

func TestObiArtFindsAFront(t *testing.T) {
	lists := fakeArchive(t)
	dir := t.TempDir()
	_, a, _ := newTestApp(t)
	a.meta = newAlbumMetadata(t, lateRegJSON)
	a.openShop(dir)
	if got := a.obiArt(ctx, lateRegMBID); got != "release/r-jp/123456" {
		t.Fatalf("got %q", got)
	}
	if got := a.obiArt(ctx, lateRegMBID); got != "release/r-jp/123456" || lists.Load() != 1 {
		t.Fatalf("second ask: %q, %d archive reads", got, lists.Load())
	}
	_, again, _ := newTestApp(t) // a restart reads obi.json
	again.meta = newAlbumMetadata(t, lateRegJSON)
	again.openShop(dir)
	if got := again.obiArt(ctx, lateRegMBID); got != "release/r-jp/123456" || lists.Load() != 1 {
		t.Fatalf("after a restart: %q, %d archive reads", got, lists.Load())
	}
	w := call(a.Routes(), "GET", "/api/shop/"+lateRegMBID+"/cover?jp=1", "")
	if w.Code != 200 || w.Body.String() != "OBI" || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("cover: %d %q", w.Code, w.Body)
	}
}

func TestObiArtNone(t *testing.T) {
	fakeArchive(t)
	_, a, _ := newTestApp(t)
	a.meta = newAlbumMetadata(t, strings.ReplaceAll(lateRegJSON, `"country":["JP"]`, `"country":["FR"]`))
	a.openShop(t.TempDir())
	if got := a.obiArt(ctx, lateRegMBID); got != "" {
		t.Fatalf("got %q", got)
	}
	if w := call(a.Routes(), "GET", "/api/shop/"+lateRegMBID+"/cover?jp=1", ""); w.Code != 404 {
		t.Fatalf("cover: %d", w.Code)
	}
}

func TestRacksMarkJapaneseCovers(t *testing.T) {
	a, _, _ := newShopApp(t)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	h := a.Routes()
	_, got, _ := getRacks(t, h, "/api/shop/racks")
	first := got.Racks[0].Crates[0].Items[0].Record
	a.obi[first.MBID] = "release/r-jp/123456"
	_, got, _ = getRacks(t, h, "/api/shop/racks")
	for _, r := range got.Racks {
		for _, c := range r.Crates {
			for _, it := range c.Items {
				if it.Record != nil && it.JP != (it.Record.MBID == first.MBID) {
					t.Errorf("%s: jp %v", it.Record.Title, it.JP)
				}
			}
		}
	}
}

// The background pass looks up each record's Japanese pressing once and keeps its covers on disk.
func TestWarmCovers(t *testing.T) {
	lists := fakeArchive(t)
	old := obiPause
	obiPause = 0
	t.Cleanup(func() { obiPause = old })
	a, _, _ := newShopApp(t)
	dir := t.TempDir()
	a.openShop(dir)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	a.meta = newAlbumMetadata(t, lateRegJSON) // knows Late Registration only: the others fail and stay unknown
	a.warmCovers(ctx)
	if got := a.obi[lateRegMBID]; got != "release/r-jp/123456" || len(a.obi) != 1 {
		t.Fatalf("obi %v", a.obi)
	}
	if _, err := os.Stat(filepath.Join(dir, "covers", "r-jp-123456")); err != nil {
		t.Fatalf("the Japanese front is not kept: %v", err)
	}
	a.warmCovers(ctx) // a known album is not asked again
	if lists.Load() != 1 {
		t.Fatalf("%d archive reads", lists.Load())
	}
}
