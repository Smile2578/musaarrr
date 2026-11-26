package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func paint(w, h int, f func(x, y int) color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, f(x, y))
		}
	}
	return img
}

func jpegOf(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A plain cover's header, worked out by hand from the reference encoder (evanw/thumbhash):
// L 22, P 43, Q 51, no AC scale, 7 luminance columns.
func TestThumbHashOfAPlainCover(t *testing.T) {
	h := thumbHash(paint(32, 32, func(x, y int) color.RGBA { return color.RGBA{200, 40, 30, 255} }))
	if len(h) != 24 || !slices.Equal(h[:5], []byte{214, 58, 3, 7, 0}) {
		t.Errorf("%v", h)
	}
}

// The hash's constant terms give back the image's average colour, as the decoder reads them.
func TestThumbHashKeepsTheAverage(t *testing.T) {
	img := paint(32, 32, func(x, y int) color.RGBA { return color.RGBA{uint8(x * 8), uint8(y * 8), uint8((x + y) * 4), 255} })
	h := thumbHash(img)
	h24 := int(h[0]) | int(h[1])<<8 | int(h[2])<<16
	l, p, q := float64(h24&63)/63, float64(h24>>6&63)/31.5-1, float64(h24>>12&63)/31.5-1
	b := l - 2.0/3*p
	r := (3*l - b + q) / 2
	g := r - q
	var sr, sg, sb float64
	for y := range 32 {
		for x := range 32 {
			c := img.RGBAAt(x, y)
			sr, sg, sb = sr+float64(c.R)/255, sg+float64(c.G)/255, sb+float64(c.B)/255
		}
	}
	for i, d := range []float64{r - sr/1024, g - sg/1024, b - sb/1024} {
		if math.Abs(d) > 0.03 {
			t.Errorf("channel %d off by %.3f", i, d)
		}
	}
}

// The dominant colour is the most common one, not the average: a red cover with a blue band stays red.
func TestDominantColor(t *testing.T) {
	img := paint(32, 32, func(x, y int) color.RGBA {
		if y < 22 {
			return color.RGBA{220, 30, 30, 255}
		}
		return color.RGBA{20, 20, 200, 255}
	})
	if got := dominant(img); got != "#dc1e1e" {
		t.Errorf("%s", got)
	}
}

func TestCoverFactsOfAJPEG(t *testing.T) {
	f, ok := coverFacts(jpegOf(t, paint(250, 250, func(x, y int) color.RGBA { return color.RGBA{30, 120, 60, 255} })))
	if th, err := base64.StdEncoding.DecodeString(f.Thumb); !ok || err != nil || len(th) != 24 || !strings.HasPrefix(f.Color, "#") {
		t.Errorf("%+v %v", f, ok)
	}
	if _, ok := coverFacts([]byte("JPEG")); ok {
		t.Error("an unreadable image has no facts")
	}
}

// The 128 px variant of a 250 px thumbnail keeps its colours.
func TestSmallCover(t *testing.T) {
	src := jpegOf(t, paint(250, 250, func(x, y int) color.RGBA {
		if x < 125 {
			return color.RGBA{240, 240, 240, 255}
		}
		return color.RGBA{10, 10, 10, 255}
	}))
	b, err := smallCover(src, 128)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil || img.Bounds().Dx() != 128 || img.Bounds().Dy() != 128 {
		t.Fatalf("%v %v", err, img.Bounds())
	}
	if l, _, _, _ := img.At(10, 64).RGBA(); l>>8 < 220 {
		t.Errorf("left %d", l>>8)
	}
	if d, _, _, _ := img.At(118, 64).RGBA(); d>>8 > 30 {
		t.Errorf("right %d", d>>8)
	}
}

// Past the cap, the covers read longest ago go first; the facts stay.
func TestPruneCovers(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i, name := range []string{"old", "mid", "new"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, make([]byte, 100), 0o600)
		os.Chtimes(p, now, now.Add(time.Duration(i-3)*time.Hour))
	}
	os.WriteFile(filepath.Join(dir, coverFactsFile), make([]byte, 500), 0o600)
	pruneCovers(dir, 250)
	left, _ := os.ReadDir(dir)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{coverFactsFile, "mid", "new"}) {
		t.Errorf("%v", names)
	}
}

// redArchive serves Kind of Blue's front as a red 250 px JPEG and counts the archive's reads.
func redArchive(t *testing.T) *atomic.Int32 {
	var asked atomic.Int32
	front := jpegOf(t, paint(250, 250, func(x, y int) color.RGBA { return color.RGBA{200, 30, 30, 255} }))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/release/bbbbbbbb-0000-4000-8000-000000000001/11-250.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(front)
	}))
	t.Cleanup(srv.Close)
	old := coverArtBase
	coverArtBase = srv.URL
	t.Cleanup(func() { coverArtBase = old })
	return &asked
}

const kindOfBlueFile = "bbbbbbbb-0000-4000-8000-000000000001-11"

// keptShop is a built shop that keeps its covers in dir.
func keptShop(t *testing.T) (*App, string) {
	a, _, _ := newShopApp(t)
	dir := t.TempDir()
	a.openShop(dir)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

// itemOf is the record mbid as the racks send it.
func itemOf(t *testing.T, h http.Handler, mbid string) RackItem {
	t.Helper()
	_, got, _ := getRacks(t, h, "/api/shop/racks")
	for _, r := range got.Racks {
		for _, c := range r.Crates {
			for _, it := range c.Items {
				if it.Record != nil && it.Record.MBID == mbid {
					return it
				}
			}
		}
	}
	t.Fatalf("%s is in no rack", mbid)
	return RackItem{}
}

// A cover's colour and ThumbHash are read as it comes in, sent with its record in the racks, and kept
// in covers/facts.jsonl: a restart knows them without asking the archive again.
func TestCoverFactsKept(t *testing.T) {
	asked := redArchive(t)
	a, dir := keptShop(t)
	h := a.Routes()
	if it := itemOf(t, h, shopID(1)); it.Color != "" || it.Thumb != "" {
		t.Fatalf("facts before the cover: %+v", it)
	}
	if w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover", ""); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	it := itemOf(t, h, shopID(1))
	if th, err := base64.StdEncoding.DecodeString(it.Thumb); !strings.HasPrefix(it.Color, "#c") || err != nil || len(th) != 24 {
		t.Fatalf("%+v", it)
	}
	again, _, _ := newShopApp(t)
	again.openShop(dir)
	if got := itemOf(t, again.Routes(), shopID(1)); got.Color != it.Color || got.Thumb != it.Thumb || asked.Load() != 1 {
		t.Fatalf("after a restart: %+v, %d archive reads", got, asked.Load())
	}
}

// A cover kept by an earlier version, without its facts, gets them the next time it is served.
func TestCoverFactsOfAKeptCover(t *testing.T) {
	asked := redArchive(t)
	a, dir := keptShop(t)
	os.WriteFile(filepath.Join(dir, "covers", kindOfBlueFile), jpegOf(t, paint(250, 250, func(x, y int) color.RGBA { return color.RGBA{20, 40, 160, 255} })), 0o600)
	h := a.Routes()
	if w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover", ""); w.Code != 200 || asked.Load() != 0 {
		t.Fatalf("%d, %d archive reads", w.Code, asked.Load())
	}
	if it := itemOf(t, h, shopID(1)); !strings.HasPrefix(it.Color, "#1") {
		t.Fatalf("%+v", it)
	}
}

// ?size=128 is the cover at 128 px for the 3D shop, made once from the kept thumbnail and kept beside it.
func TestSmallCoverServed(t *testing.T) {
	asked := redArchive(t)
	a, dir := keptShop(t)
	h := a.Routes()
	for range 2 {
		w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover?size=128", "")
		img, err := jpeg.Decode(w.Body)
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || err != nil || img.Bounds().Dx() != 128 {
			t.Fatalf("%d %v %v", w.Code, w.Header(), err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "covers", kindOfBlueFile+"-128")); err != nil || asked.Load() != 1 {
		t.Fatalf("%v, %d archive reads", err, asked.Load())
	}
	w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover", "")
	if img, err := jpeg.Decode(w.Body); err != nil || img.Bounds().Dx() != 250 {
		t.Fatalf("the thumbnail itself: %v", err)
	}
}

// Serving a kept cover marks it read, so the cap takes the covers nobody looked at for longest.
func TestCoverReadStaysFresh(t *testing.T) {
	redArchive(t)
	a, dir := keptShop(t)
	h := a.Routes()
	call(h, "GET", "/api/shop/"+shopID(1)+"/cover", "")
	path := filepath.Join(dir, "covers", kindOfBlueFile)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(path, old, old)
	call(h, "GET", "/api/shop/"+shopID(1)+"/cover", "")
	if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) > time.Hour {
		t.Fatalf("%v %v", err, info.ModTime())
	}
}

// The background pass ends by bringing the covers back under the cap; their facts stay.
func TestWarmCoversKeepsTheCap(t *testing.T) {
	redArchive(t)
	old, oldPause := coverCap, obiPause
	coverCap, obiPause = 0, 0
	t.Cleanup(func() { coverCap, obiPause = old, oldPause })
	a, dir := keptShop(t)
	a.meta = nil // no Japanese pressings looked up
	a.warmCovers(ctx)
	left, _ := os.ReadDir(filepath.Join(dir, "covers"))
	if len(left) != 1 || left[0].Name() != coverFactsFile {
		t.Fatalf("%v", left)
	}
}
