package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // the archive keeps a few covers in PNG
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The covers the shop keeps, in DATA_DIR/covers: each archive thumbnail (250 px), its 128 px
// variant for the 3D shop once asked for, and the facts read from it when it came in: its
// dominant colour and its ThumbHash, which the racks send along so a crate shows its sleeves'
// colours before their covers load. The folder stays under a cap: the covers read longest ago go.

// coverFactsFile holds a JSON line per cover read: {"art":…,"color":"#rrggbb","thumb":base64}.
const coverFactsFile = "facts.jsonl"

// coverCap is the most the covers may take on disk: about 30 000 thumbnails.
// shortcut: a fixed 1 GB; a setting if a small disk ever needs less.
var coverCap int64 = 1 << 30

type coverFact struct {
	Art   string `json:"art,omitempty"`
	Color string `json:"color"`
	Thumb string `json:"thumb"`
}

// coverFacts reads a cover's dominant colour and ThumbHash; false when it is no image Go can read.
func coverFacts(b []byte) (coverFact, bool) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return coverFact{}, false
	}
	w, h := fit(img.Bounds(), 32)
	tiny := shrink(img, w, h)
	return coverFact{Color: dominant(tiny), Thumb: base64.StdEncoding.EncodeToString(thumbHash(tiny))}, true
}

// smallCover is the cover b scaled down to size pixels on its longer side, as a JPEG.
func smallCover(b []byte, size int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	w, h := fit(img.Bounds(), size)
	var out bytes.Buffer
	err = jpeg.Encode(&out, shrink(img, w, h), &jpeg.Options{Quality: 82})
	return out.Bytes(), err
}

// fit is the size of r scaled down to n pixels on its longer side; never larger than r.
func fit(r image.Rectangle, n int) (int, int) {
	w, h := r.Dx(), r.Dy()
	if m := max(w, h); m > n {
		w, h = max(1, w*n/m), max(1, h*n/m)
	}
	return w, h
}

// shrink averages the pixels of img that fall in each pixel of a w×h image (a box filter).
func shrink(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+max((y+1)*b.Dy()/h, y*b.Dy()/h+1)
		for x := range w {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+max((x+1)*b.Dx()/w, x*b.Dx()/w+1)
			var s [3]uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, bl, _ := img.At(sx, sy).RGBA()
					s[0], s[1], s[2] = s[0]+uint64(r>>8), s[1]+uint64(g>>8), s[2]+uint64(bl>>8)
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = uint8(s[0]/n), uint8(s[1]/n), uint8(s[2]/n), 255
		}
	}
	return out
}

// dominant is the mean of the most common colour of img, counted in 4 bits a channel.
func dominant(img *image.RGBA) string {
	var count [4096]int
	var sum [4096][3]int
	p := img.Pix
	for i := 0; i+3 < len(p); i += 4 {
		k := int(p[i]>>4)<<8 | int(p[i+1]>>4)<<4 | int(p[i+2]>>4)
		count[k]++
		sum[k][0], sum[k][1], sum[k][2] = sum[k][0]+int(p[i]), sum[k][1]+int(p[i+1]), sum[k][2]+int(p[i+2])
	}
	k := 0
	for i, n := range count {
		if n > count[k] {
			k = i
		}
	}
	n := max(count[k], 1)
	return fmt.Sprintf("#%02x%02x%02x", sum[k][0]/n, sum[k][1]/n, sum[k][2]/n)
}

// thumbHash encodes img (at most 100×100) as a ThumbHash, the way evanw/thumbhash's rgbaToThumbHash
// does for an opaque image: covers have no transparency, so alpha is not encoded.
func thumbHash(img *image.RGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	l, p, q := make([]float64, w*h), make([]float64, w*h), make([]float64, w*h)
	for i := range w * h {
		r, g, b := float64(img.Pix[4*i])/255, float64(img.Pix[4*i+1])/255, float64(img.Pix[4*i+2])/255
		l[i], p[i], q[i] = (r+g+b)/3, (r+g)/2-b, r-g
	}
	round := func(x float64) int { return int(math.Floor(x + 0.5)) } // JavaScript's Math.round
	lx := max(1, round(7*float64(w)/float64(max(w, h))))
	ly := max(1, round(7*float64(h)/float64(max(w, h))))
	ldc, lac, lscale := dctChannel(l, w, h, max(3, lx), max(3, ly))
	pdc, pac, pscale := dctChannel(p, w, h, 3, 3)
	qdc, qac, qscale := dctChannel(q, w, h, 3, 3)
	landscape := 0
	if w > h {
		landscape = 1
	}
	h24 := round(63*ldc) | round(31.5+31.5*pdc)<<6 | round(31.5+31.5*qdc)<<12 | round(31*lscale)<<18
	h16 := lx | round(63*pscale)<<3 | round(63*qscale)<<9 | landscape<<15
	if landscape == 1 {
		h16 = ly | h16&^7
	}
	hash := []byte{byte(h24), byte(h24 >> 8), byte(h24 >> 16), byte(h16), byte(h16 >> 8)}
	for i, f := range slices.Concat(lac, pac, qac) {
		if i%2 == 0 {
			hash = append(hash, 0)
		}
		hash[len(hash)-1] |= byte(round(15*f) << (i % 2 * 4))
	}
	return hash
}

// dctChannel is the DCT of channel (w×h) up to nx×ny terms: its constant term, its varying
// terms brought into [0, 1], and their scale.
func dctChannel(channel []float64, w, h, nx, ny int) (dc float64, ac []float64, scale float64) {
	fx := make([]float64, w)
	for cy := range ny {
		for cx := 0; cx*ny < nx*(ny-cy); cx++ {
			for x := range w {
				fx[x] = math.Cos(math.Pi / float64(w) * float64(cx) * (float64(x) + 0.5))
			}
			f := 0.0
			for y := range h {
				fy := math.Cos(math.Pi / float64(h) * float64(cy) * (float64(y) + 0.5))
				for x := range w {
					f += channel[x+y*w] * fx[x] * fy
				}
			}
			f /= float64(w * h)
			if cx == 0 && cy == 0 {
				dc = f
				continue
			}
			ac = append(ac, f)
			scale = max(scale, math.Abs(f))
		}
	}
	if scale > 0 {
		for i := range ac {
			ac[i] = 0.5 + 0.5/scale*ac[i]
		}
	}
	return dc, ac, scale
}

// pruneCovers deletes the covers of dir read longest ago until they take at most limit bytes.
// The facts stay: they still describe a cover fetched again later.
func pruneCovers(dir string, limit int64) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type file struct {
		path string
		size int64
		mod  int64
	}
	var files []file
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || e.Name() == coverFactsFile || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.Size(), info.ModTime().UnixNano()})
		total += info.Size()
	}
	slices.SortFunc(files, func(a, b file) int { return cmp.Compare(a.mod, b.mod) })
	for _, f := range files {
		if total <= limit {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
	return nil
}

// shopCover relays the Cover Art Archive's 250 px thumbnail, so the page only loads images from Musaarr.
// With ?jp=1 it is the front of a Japanese pressing, or a 404 when the album has none; with ?size=128,
// the same at 128 px for the 3D shop.
func (a *App) shopCover(w http.ResponseWriter, r *http.Request) {
	id, ok := validMBID(r.PathValue("mbid"))
	var art string
	switch st := a.shopNow(); {
	case !ok:
	case r.URL.Query().Get("jp") == "1":
		art = a.obiArt(r.Context(), id)
	case st != nil:
		art = st.art[id]
	}
	if art == "" {
		http.NotFound(w, r)
		return
	}
	thumb := a.shopThumb
	if r.URL.Query().Get("size") == "128" {
		thumb = a.smallThumb
	}
	b, ct, err := thumb(r.Context(), art)
	if errors.Is(err, errNoCover) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("shop, cover %s: %v", id, err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Write(b)
}

var errNoCover = errors.New("le Cover Art Archive n'a pas cette image")

// coverPath is where art's thumbnail is kept: archive image ids never change, so the file stays good.
// It is "" without a data folder.
func (a *App) coverPath(art string) string {
	name := strings.NewReplacer("release/", "", "/", "-").Replace(art)
	if a.coverDir == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return ""
	}
	return filepath.Join(a.coverDir, name)
}

// shopThumb is art's 250 px thumbnail, from DATA_DIR/covers once it has been fetched from the archive.
func (a *App) shopThumb(ctx context.Context, art string) ([]byte, string, error) {
	path := a.coverPath(art)
	if b, ok := readKept(path); ok {
		a.noteCover(art, b) // kept before its facts were read
		return b, http.DetectContentType(b), nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", coverArtBase+"/"+art+"-250.jpg", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Musaarr")
	resp, err := coverClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", errNoCover
	}
	ct, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	if ct = strings.TrimSpace(ct); resp.StatusCode != 200 || !coverTypes[ct] {
		return nil, "", fmt.Errorf("HTTP %d, type %q", resp.StatusCode, ct)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	keepCover(path, b)
	a.noteCover(art, b)
	return b, ct, nil
}

// smallThumb is art's cover at 128 px for the 3D shop, made once from its thumbnail and kept beside
// it; an image Go cannot read (WebP, GIF) is sent as it is.
func (a *App) smallThumb(ctx context.Context, art string) ([]byte, string, error) {
	path := a.coverPath(art)
	if path != "" {
		path += "-128"
	}
	if b, ok := readKept(path); ok {
		return b, http.DetectContentType(b), nil
	}
	b, ct, err := a.shopThumb(ctx, art)
	if err != nil {
		return nil, "", err
	}
	small, err := smallCover(b, 128)
	if err != nil {
		return b, ct, nil
	}
	keepCover(path, small)
	return small, "image/jpeg", nil
}

// readKept is the cover kept at path. One last read more than a day ago is marked read again, so
// the cap takes the covers nobody looked at for longest.
func readKept(path string) ([]byte, bool) {
	b, err := os.ReadFile(path)
	if err != nil || !coverTypes[http.DetectContentType(b)] {
		return nil, false
	}
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
		now := time.Now()
		os.Chtimes(path, now, now)
	}
	return b, true
}

// keepCover writes b at path; "" keeps nothing. A failure is only a cover fetched again next time.
func keepCover(path string, b []byte) {
	if path == "" {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp") // one per fetch: the page and the pass may fetch the same cover
	if err == nil {
		_, err = f.Write(b)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(f.Name(), path)
		}
		if err != nil {
			os.Remove(f.Name())
		}
	}
	if err != nil {
		log.Printf("shop, cover %s: %v", path, err)
	}
}

// loadFacts reads covers/facts.jsonl; a line torn by a crash only means that cover is read again.
func (a *App) loadFacts() {
	f, err := os.Open(filepath.Join(a.coverDir, coverFactsFile))
	if err != nil {
		return
	}
	defer f.Close()
	a.factsMu.Lock()
	defer a.factsMu.Unlock()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var c coverFact
		if json.Unmarshal(sc.Bytes(), &c) == nil && c.Art != "" {
			a.facts[c.Art] = c
		}
	}
}

// noteCover reads the facts of art's cover b the first time it comes in, and keeps them.
func (a *App) noteCover(art string, b []byte) {
	a.factsMu.Lock()
	_, known := a.facts[art]
	a.factsMu.Unlock()
	if known {
		return
	}
	c, ok := coverFacts(b)
	if !ok {
		return
	}
	c.Art = art
	line, err := json.Marshal(c)
	a.factsMu.Lock()
	defer a.factsMu.Unlock()
	if _, known := a.facts[art]; known || err != nil { // the page and the pass read it at once
		return
	}
	a.facts[art] = c
	if a.coverDir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(a.coverDir, coverFactsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		log.Printf("shop, %s: %v", coverFactsFile, err)
	}
}
