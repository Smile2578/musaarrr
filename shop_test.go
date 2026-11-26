package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func shopID(i int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", i) }

// lbShop answers like ListenBrainz's sitewide statistics and release group metadata.
type lbShop struct {
	mu        sync.Mutex
	groups    []string            // sitewide release groups, most listened first, as JSON
	charts    map[string][]string // the same for the ranges other than all_time: "this_month", "month"
	chartDown bool                // the ranges other than all_time answer 502
	meta      map[string]string   // release group mbid → its metadata, as JSON
	down      bool
	sitewide  int
	metaSizes []int
	srv       *httptest.Server
}

func newLBShop(t *testing.T) *lbShop {
	f := &lbShop{meta: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Token "+testListensToken {
			w.WriteHeader(401)
			return
		}
		if f.down {
			w.WriteHeader(502)
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/1/stats/sitewide/release-groups":
			f.sitewide++
			off, _ := strconv.Atoi(q.Get("offset"))
			n, _ := strconv.Atoi(q.Get("count"))
			rng, list := q.Get("range"), f.groups
			switch {
			case n < 1 || (rng != "all_time" && rng != "this_month" && rng != "month"):
				w.WriteHeader(400)
				return
			case rng != "all_time" && f.chartDown:
				w.WriteHeader(502)
				return
			case rng != "all_time":
				list = f.charts[rng]
				if len(list) == 0 {
					w.WriteHeader(204) // ListenBrainz has not computed these statistics yet
					return
				}
			}
			page := list[min(off, len(list)):min(off+n, len(list))]
			fmt.Fprintf(w, `{"payload":{"count":%d,"offset":%d,"range":%q,"release_groups":[%s]}}`, len(page), off, rng, strings.Join(page, ","))
		case "/1/metadata/release_group/":
			if q.Get("inc") != "artist tag" {
				w.WriteHeader(400)
				return
			}
			ids := strings.Split(q.Get("release_group_mbids"), ",")
			f.metaSizes = append(f.metaSizes, len(ids))
			var parts []string
			for _, id := range ids {
				if m, ok := f.meta[id]; ok {
					parts = append(parts, `"`+id+`":`+m)
				}
			}
			fmt.Fprintf(w, "{%s}", strings.Join(parts, ","))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// group adds a release group to the sitewide top (when listens > 0) and to the metadata.
// Tags read "tag=count"; artist tags come after a "|".
func (f *lbShop) group(id, title, artist string, listens int64, typ, date string, caa int64, tags ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	artistMBID := "aaaaaaaa-0000-4000-8000-" + id[24:]
	if listens > 0 {
		f.groups = append(f.groups, groupJSON(id, title, artist, listens, caa))
	}
	var rg, ar []string
	list := &rg
	for _, t := range tags {
		if t == "|" {
			list = &ar
			continue
		}
		name, n, _ := strings.Cut(t, "=")
		*list = append(*list, fmt.Sprintf(`{"count":%s,"genre_mbid":"","tag":%q}`, n, name))
	}
	f.meta[id] = fmt.Sprintf(`{"artist":{"artists":[{"artist_mbid":%q,"name":%q}],"name":%q},
	 "release_group":{"caa_id":%d,"caa_release_mbid":%q,"date":%q,"name":%q,"type":%q},
	 "tag":{"artist":[%s],"release_group":[%s]}}`, artistMBID, artist, artist, caa, "bbbbbbbb-0000-4000-8000-"+id[24:], date, title, typ,
		strings.Join(ar, ","), strings.Join(rg, ","))
}

func groupJSON(id, title, artist string, listens, caa int64) string {
	return fmt.Sprintf(`{"artist_mbids":[%q],"artist_name":%q,"caa_id":%d,"caa_release_mbid":%q,
	 "listen_count":%d,"release_group_mbid":%q,"release_group_name":%q}`, "aaaaaaaa-0000-4000-8000-"+id[24:], artist, caa, "bbbbbbbb-0000-4000-8000-"+id[24:], listens, id, title)
}

// chart puts release groups, most listened first, in the sitewide statistics of a range; their
// metadata comes from group.
func (f *lbShop) chart(rng string, groups ...[3]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.charts == nil {
		f.charts = map[string][]string{}
	}
	for i, g := range groups {
		f.charts[rng] = append(f.charts[rng], groupJSON(g[0], g[1], g[2], int64(1000-i), 0))
	}
}

func quickShop(t *testing.T, top, page int) {
	oldTop, oldPage, oldPause, oldRetry := shopTop, shopPage, shopPause, shopRetry
	shopTop, shopPage, shopPause, shopRetry = top, page, 0, 0
	t.Cleanup(func() { shopTop, shopPage, shopPause, shopRetry = oldTop, oldPage, oldPause, oldRetry })
}

type shopAnswer struct {
	Records []ShopRecord
	Bins    []struct {
		ID string
		N  int
	}
	Built string
}

// newShopApp is the suggestion app (Ye in Lidarr, Late Registration in Plex) after a suggestion
// pass, then pointed at a ListenBrainz that knows the whole site.
func newShopApp(t *testing.T) (*App, *lbShop, func() (int, shopAnswer, string)) {
	quickShop(t, 10, 2)
	a, _ := newSuggestApp(t)
	a.refreshSuggestions(ctx) // the user's artists: Ye's Late Registration and Yeezus
	lb := newLBShop(t)
	a.listens = NewListens(lb.srv.URL, testListensToken)
	lb.group(shopID(1), "Kind of Blue", "Miles Davis", 900, "Album", "1959-08-17", 11, "seen live=12", "hard bop=7")
	lb.group(shopID(2), "To Pimp a Butterfly", "Kendrick Lamar", 800, "Album", "2015-03-15", 12, "jazz rap=3", "conscious hip hop=8")
	lb.group(shopID(3), "Power", "Ye", 700, "Single", "2010", 13, "hip hop=9")
	lb.group(lateRegistration, "Late Registration", "Ye", 600, "Album", "2005-08-30", 14, "hip hop=4")
	lb.group(shopID(5), "Histoire de Melody Nelson", "Serge Gainsbourg", 500, "Album", "1971", 15, "|", "chanson française=6")
	lb.group(shopID(6), "Mystery", "Nobody", 400, "Album", "", 0, "2010s=3")
	lb.group(yeezus, "Yeezus", "Ye", 0, "Album", "2013-06-18", 16, "industrial hip hop=5")
	h := a.Routes()
	return a, lb, func() (int, shopAnswer, string) {
		w := call(h, "GET", "/api/shop", "")
		var got shopAnswer
		json.Unmarshal(w.Body.Bytes(), &got)
		return w.Code, got, errCode(w)
	}
}

func TestShopBuild(t *testing.T) {
	a, lb, get := newShopApp(t)
	if code, _, msg := get(); code != 503 || msg != "shop.filling" {
		t.Fatalf("before the first build: %d %q", code, msg)
	}
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	if lb.sitewide < 2 {
		t.Errorf("sitewide pages: %d", lb.sitewide)
	}
	code, got, _ := get()
	var titles []string
	for _, r := range got.Records {
		titles = append(titles, r.Title)
	}
	// Power is a single; Late Registration is in the top and among the user's artists, kept once with
	// its larger count; Yeezus comes from the user's artists only.
	if code != 200 || strings.Join(titles, ",") != "Late Registration,Yeezus,Kind of Blue,To Pimp a Butterfly,Histoire de Melody Nelson,Mystery" {
		t.Fatalf("%d %v", code, titles)
	}
	kob := got.Records[2]
	if kob.MBID != shopID(1) || kob.Artist != "Miles Davis" || kob.ArtistMBID == "" || kob.Year != 1959 || kob.Bin != "jazz" || kob.Style != "hard bop" ||
		kob.Listens != 900 || kob.Cover != "/api/shop/"+shopID(1)+"/cover" || kob.Plex {
		t.Errorf("kind of blue %+v", kob)
	}
	if r := got.Records[0]; !r.Plex || r.Bin != "hiphop" || r.Listens != 3300000 {
		t.Errorf("late registration %+v", r)
	}
	if r := got.Records[1]; r.Year != 2013 || r.Bin != "hiphop" || r.Style != "industrial hip hop" || r.ArtistMBID != yeMBID || r.Plex || r.Cover == "" {
		t.Errorf("yeezus %+v", r)
	}
	if r := got.Records[3]; r.Bin != "hiphop" || r.Style != "conscious hip hop" {
		t.Errorf("tags by count: %+v", r)
	}
	if r := got.Records[4]; r.Bin != "chanson" || r.Style != "chanson française" {
		t.Errorf("artist tags: %+v", r)
	}
	if r := got.Records[5]; r.Bin != "divers" || r.Style != "" || r.Cover != "" || r.Year != 0 {
		t.Errorf("no genre, no art: %+v", r)
	}
	bins := map[string]int{}
	for _, b := range got.Bins {
		bins[b.ID] = b.N
	}
	if got.Bins[0].ID != "jazz" || bins["jazz"] != 1 || bins["hiphop"] != 3 || bins["chanson"] != 1 || bins["divers"] != 1 || bins["rock"] != 0 || got.Built == "" {
		t.Errorf("bins %+v built %q", got.Bins, got.Built)
	}
}

func TestBinFor(t *testing.T) {
	for tag, want := range map[string]string{
		"hard bop": "jazz", "conscious hip hop": "hiphop", "chanson française": "chanson", "jazz rap": "hiphop",
		"French House": "electro", "post-punk": "indie", "punk rock": "punk", "blues rock": "rock", "rhythm and blues": "soul",
		"trip hop": "electro", "Bossa Nova": "latin", "soundtrack": "bo", "afrobeat": "afro", "singer-songwriter": "folk",
		"contemporary classical": "classique", "disco": "funk", "dream pop": "indie", "seen live": "divers",
		"Indie Rock": "indie", "Alternative Rock": "indie", "heavy metal": "metal", "hardcore punk": "punk", "City Pop": "japon",
		"J-pop": "japon", "Ambient": "ambient", "Experimental": "ambient", "roots reggae": "reggae", "Lovers Rock": "reggae",
		"zouk": "afro", "Fado": "monde", "Folk, World, & Country": "monde", "Indian Classical": "monde", "country": "country",
		"Country Rock": "country", "bluegrass": "country", "G-Funk": "hiphop", "Romantic": "classique", "Easy Listening": "pop",
	} {
		if got, _ := binFor([]lbTag{{Tag: tag, Count: 1}}); got != want {
			t.Errorf("%q: %q, want %q", tag, got, want)
		}
	}
	bin, style := binFor([]lbTag{{Tag: "albums i own", Count: 9}, {Tag: "Cool Jazz", Count: 2}, {Tag: "rock", Count: 1}})
	if bin != "jazz" || style != "cool jazz" {
		t.Errorf("first genre by count: %q %q", bin, style)
	}
}

func TestShopMetaInBatches(t *testing.T) {
	a, lb, get := newShopApp(t)
	quickShop(t, 200, 100)
	for i := 10; i < 124; i++ {
		lb.group(shopID(i), "Album "+strconv.Itoa(i), "Artist", int64(1000-i), "Album", "1999", 0, "rock=1")
	}
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range lb.metaSizes {
		if n > 50 {
			t.Errorf("batch of %d", n)
		}
		total += n
	}
	if _, got, _ := get(); total != 121 || len(got.Records) != 120 {
		t.Errorf("asked %d, kept %d", total, len(got.Records))
	}
}

func TestShopKeepsLastStock(t *testing.T) {
	a, lb, get := newShopApp(t)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	_, before, _ := get()
	lb.mu.Lock()
	lb.down = true
	lb.mu.Unlock()
	if err := a.buildShop(ctx); err == nil {
		t.Fatal("a failed build is an error")
	}
	if code, after, _ := get(); code != 200 || len(after.Records) != len(before.Records) || after.Built != before.Built {
		t.Errorf("%d %d records, built %q then %q", code, len(after.Records), before.Built, after.Built)
	}
}

func TestShopWithoutListenBrainz(t *testing.T) {
	_, a, h := newTestApp(t)
	a.listens = nil
	w := call(h, "GET", "/api/shop", "")
	if w.Code != 503 || errCode(w) != "shop.noSource" {
		t.Errorf("%d %q", w.Code, errMsg(w))
	}
	a.base = []ShopRecord{{MBID: shopID(1), Title: "Kind of Blue", Bin: "jazz"}} // a kept stock, STOCK_URL off
	if w := call(h, "GET", "/api/shop", ""); w.Code != 503 || errCode(w) != "shop.filling" {
		t.Errorf("kept stock: %d %q", w.Code, errMsg(w))
	}
	a.base = nil
	a.stockURL = "https://example.invalid/stock.json.gz" // the stock is on its way
	if w := call(h, "GET", "/api/shop", ""); w.Code != 503 || errCode(w) != "shop.filling" {
		t.Errorf("%d %q", w.Code, errMsg(w))
	}
	if err := a.buildShop(ctx); err != nil {
		t.Error(err)
	}
}

func TestShopEncoding(t *testing.T) {
	a, _, _ := newShopApp(t)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	h := a.Routes()
	req := httptest.NewRequest("GET", "/api/shop", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	tag := w.Header().Get("ETag")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || tag == "" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); !bytes.Contains(b, []byte(`"Kind of Blue"`)) {
		t.Errorf("body %s", b)
	}
	req.Header.Set("If-None-Match", tag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 304 {
		t.Errorf("revalidation: %d", w.Code)
	}
}

func TestShopCover(t *testing.T) {
	a, _, _ := newShopApp(t)
	ct := "image/jpeg"
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/release/bbbbbbbb-0000-4000-8000-000000000001/11-250.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Write([]byte("JPEG"))
	}))
	t.Cleanup(art.Close)
	old := coverArtBase
	coverArtBase = art.URL
	t.Cleanup(func() { coverArtBase = old })
	h := a.Routes()
	if w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover", ""); w.Code != 503 && w.Code != 404 {
		t.Errorf("before the first build: %d", w.Code)
	}
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover", "")
	if w.Code != 200 || w.Body.String() != "JPEG" || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "private, max-age=604800" {
		t.Fatalf("%d %q %v", w.Code, w.Body, w.Header())
	}
	for _, id := range []string{shopID(6), shopID(99), "nope", shopID(2)} { // no art, unknown, not an id, the archive lacks it
		if w := call(h, "GET", "/api/shop/"+id+"/cover", ""); w.Code != 404 {
			t.Errorf("%s: %d", id, w.Code)
		}
	}
	ct = "image/svg+xml"
	if w := call(h, "GET", "/api/shop/"+shopID(1)+"/cover", ""); w.Code != 502 {
		t.Errorf("svg: %d", w.Code)
	}
}

// A restart (every deploy) keeps the stock: the build takes minutes.
func TestShopSurvivesRestart(t *testing.T) {
	a, _, _ := newShopApp(t)
	dir := t.TempDir()
	a.openShop(dir)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	b := NewApp(a.svc)
	b.listens = a.listens
	b.openShop(dir)
	w := call(b.Routes(), "GET", "/api/shop", "")
	var got shopAnswer
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || len(got.Records) != 6 || got.Records[2].Cover == "" {
		t.Fatalf("%d %+v", w.Code, got)
	}
	if b.shopNow().art[shopID(1)] == "" {
		t.Error("cover lost")
	}
}

func TestShopDue(t *testing.T) {
	a := &App{}
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if !a.shopDue(now) {
		t.Fatal("no stock: due")
	}
	a.setShop(nil, now.Add(-2*time.Hour), false)
	if a.shopDue(now) {
		t.Fatal("built 2 h ago: not due") // a restart or Plex down does not rebuild it
	}
	a.setShop(nil, now.Add(-24*time.Hour), false)
	if !a.shopDue(now) {
		t.Fatal("built yesterday: due")
	}
}

type racksAnswer struct {
	Racks []Rack
}

func getRacks(t *testing.T, h http.Handler, path string) (int, racksAnswer, string) {
	t.Helper()
	w := call(h, "GET", path, "")
	var got racksAnswer
	json.Unmarshal(w.Body.Bytes(), &got)
	return w.Code, got, errCode(w)
}

func TestShopRacksAPI(t *testing.T) {
	a, _, _ := newShopApp(t)
	h := a.Routes()
	if code, _, msg := getRacks(t, h, "/api/shop/racks"); code != 503 || msg != "shop.filling" {
		t.Fatalf("before the first build: %d %q", code, msg)
	}
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	count := func(rs []Rack) (n, owned int, titles []string) {
		for _, r := range rs {
			for _, c := range r.Crates {
				for _, it := range c.Items {
					if it.Record != nil {
						n++
						titles = append(titles, it.Record.Title)
						if it.Owned {
							owned++
						}
					}
				}
			}
		}
		return
	}
	code, got, _ := getRacks(t, h, "/api/shop/racks")
	n, owned, _ := count(got.Racks)
	if code != 200 || n != 6 || owned != 1 || got.Racks[0].ID != "jazz" {
		t.Fatalf("%d: %d records, %d owned, %+v", code, n, owned, got.Racks)
	}
	code, got, _ = getRacks(t, h, "/api/shop/racks?hide=1")
	n, owned, titles := count(got.Racks)
	if code != 200 || n != 5 || owned != 0 || slices.Contains(titles, "Late Registration") {
		t.Fatalf("hidden: %d, %d records, %v", code, n, titles)
	}
}

// An artist written in kanji gets MusicBrainz's sort name at the build; artists in Latin letters are not asked.
func TestShopSortNames(t *testing.T) {
	a, lb, _ := newShopApp(t)
	lb.group(shopID(20), "The Rainbow Goblins", "高中正義", 650, "Album", "1981", 30, "city pop=5")
	lb.group(shopID(21), "Brasilian Skies", "高中正義", 640, "Album", "1978", 31, "city pop=4")
	var asked atomic.Int32
	md := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if !strings.HasPrefix(r.URL.Path, "/artist/aaaaaaaa-0000-4000-8000-0000000000") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"artistname":"高中正義","sortname":"Takanaka, Masayoshi","Albums":[]}`))
	}))
	t.Cleanup(md.Close)
	a.meta = NewMetadata(md.URL)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range a.shopNow().recs {
		if want := map[bool]string{true: "Takanaka, Masayoshi"}[r.Artist == "高中正義"]; r.Sort != want {
			t.Errorf("%s: sort %q", r.Title, r.Sort)
		}
	}
	if asked.Load() != 2 { // the fake gives each record its own artist
		t.Fatalf("%d metadata calls", asked.Load())
	}
}

// A cover is fetched from the archive once, then served from DATA_DIR/covers, a restart included.
func TestShopCoverKeptOnDisk(t *testing.T) {
	a, _, _ := newShopApp(t)
	dir := t.TempDir()
	a.openShop(dir)
	var asked atomic.Int32
	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Path != "/release/bbbbbbbb-0000-4000-8000-000000000001/11-250.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xff\xd8\xff\xe0 JPEG"))
	}))
	t.Cleanup(art.Close)
	old := coverArtBase
	coverArtBase = art.URL
	t.Cleanup(func() { coverArtBase = old })
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if w := call(a.Routes(), "GET", "/api/shop/"+shopID(1)+"/cover", ""); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || !strings.HasSuffix(w.Body.String(), "JPEG") {
			t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body)
		}
	}
	if asked.Load() != 1 {
		t.Fatalf("%d archive reads", asked.Load())
	}
	again, _, _ := newShopApp(t)
	again.openShop(dir)
	if w := call(again.Routes(), "GET", "/api/shop/"+shopID(1)+"/cover", ""); w.Code != 200 || asked.Load() != 1 {
		t.Fatalf("after a restart: %d, %d archive reads", w.Code, asked.Load())
	}
}

func rackIDs(rs []Rack) []string {
	var ids []string
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	return ids
}

func rackTitles(r Rack) []string {
	var out []string
	for _, c := range r.Crates {
		for _, it := range c.Items {
			if it.Record != nil {
				out = append(out, it.Record.Title)
			}
		}
	}
	return out
}

// This month's most listened albums on ListenBrainz stand in a « Ce mois-ci » rack at the entrance,
// in the chart's order, and in their genre's rack too; singles stay out.
func TestShopMonthRack(t *testing.T) {
	a, lb, _ := newShopApp(t)
	gnx := shopID(7)
	lb.group(gnx, "GNX", "Kendrick Lamar", 0, "Album", "2024-11-22", 17, "west coast hip hop=6")
	lb.chart("this_month", [3]string{gnx, "GNX", "Kendrick Lamar"}, [3]string{shopID(3), "Power", "Ye"}, [3]string{shopID(1), "Kind of Blue", "Miles Davis"})
	lb.chart("month", [3]string{shopID(5), "Histoire de Melody Nelson", "Serge Gainsbourg"})
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	code, got, _ := getRacks(t, a.Routes(), "/api/shop/racks")
	if code != 200 || len(got.Racks) == 0 {
		t.Fatalf("%d %+v", code, got)
	}
	mois := got.Racks[0]
	if mois.ID != "mois" || mois.Name != "Ce mois-ci" || mois.N != 2 || !slices.Equal(rackTitles(mois), []string{"GNX", "Kind of Blue"}) {
		t.Fatalf("first rack: %s %q %d %v", mois.ID, mois.Name, mois.N, rackTitles(mois))
	}
	if labels := crateLabels(mois.Crates); !slices.Equal(labels, []string{"1–2"}) {
		t.Errorf("labels %v", labels)
	}
	for _, r := range got.Racks[1:] {
		if r.ID == "hiphop" && !slices.Contains(rackTitles(r), "GNX") {
			t.Errorf("GNX is not in the hip-hop rack: %v", rackTitles(r))
		}
	}
}

// In the first days of a month ListenBrainz has no statistics for it yet: last month's chart fills the rack.
func TestShopMonthRackLastMonth(t *testing.T) {
	a, lb, _ := newShopApp(t)
	lb.chart("month", [3]string{shopID(5), "Histoire de Melody Nelson", "Serge Gainsbourg"})
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	_, got, _ := getRacks(t, a.Routes(), "/api/shop/racks")
	if got.Racks[0].ID != "mois" || !slices.Equal(rackTitles(got.Racks[0]), []string{"Histoire de Melody Nelson"}) {
		t.Fatalf("racks %v, first %v", rackIDs(got.Racks), rackTitles(got.Racks[0]))
	}
}

// A month chart that does not answer leaves the rack out, not the rest of the shop.
func TestShopMonthRackDown(t *testing.T) {
	a, lb, _ := newShopApp(t)
	lb.chart("this_month", [3]string{shopID(1), "Kind of Blue", "Miles Davis"})
	lb.chartDown = true
	if err := a.buildShop(ctx); err != nil {
		t.Fatalf("the build failed: %v", err)
	}
	_, got, _ := getRacks(t, a.Routes(), "/api/shop/racks")
	if ids := rackIDs(got.Racks); len(ids) == 0 || ids[0] != "jazz" || slices.Contains(ids, "mois") {
		t.Fatalf("racks %v", ids)
	}
}
