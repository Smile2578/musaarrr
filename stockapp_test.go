package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// baseStock is a starting stock as the CI writes it: Waltz for Debby, Kind of Blue (also in
// ListenBrainz's charts) and Late Registration (in the test Plex).
func baseStock(t *testing.T, dir string) {
	t.Helper()
	recs := []ShopRecord{
		{MBID: shopID(21), Title: "Waltz for Debby", Artist: "Bill Evans", ArtistMBID: shopID(921), Year: 1962, Bin: "jazz", Style: "cool jazz"},
		{MBID: shopID(1), Title: "Kind of Blue", Artist: "Miles Davis", ArtistMBID: shopID(901), Year: 1959, Bin: "jazz", Style: "modal"},
		{MBID: lateRegistration, Title: "Late Registration", Artist: "Ye", ArtistMBID: yeMBID, Year: 2005, Bin: "hiphop", Layer: "deep"},
	}
	if err := writeStock(filepath.Join(dir, "stock.json.gz"), recs, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
}

func recTitles(recs []ShopRecord) []string {
	var out []string
	for _, r := range recs {
		out = append(out, r.Title)
	}
	return out
}

// Without a ListenBrainz token, the shop is the starting stock alone.
func TestShopFromStockAlone(t *testing.T) {
	a, _, get := newShopApp(t)
	a.listens = nil
	dir := t.TempDir()
	baseStock(t, dir)
	a.openShop(dir)
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	code, got, _ := get()
	if code != 200 || !slices.Equal(recTitles(got.Records), []string{"Waltz for Debby", "Kind of Blue", "Late Registration"}) {
		t.Fatalf("%d %v", code, recTitles(got.Records))
	}
	w := got.Records[0]
	if w.Cover != "/api/shop/"+shopID(21)+"/cover" || w.Plex || w.Style != "cool jazz" || !got.Records[2].Plex || got.Records[2].Layer != "deep" {
		t.Errorf("%+v", got.Records)
	}
	if art := a.shopNow().art[shopID(21)]; art != "release-group/"+shopID(21)+"/front" {
		t.Errorf("pochette : %q", art)
	}
}

// With a token, this month's chart and the user's artists come on top of the stock; the site's
// all-time top is no longer read, and a record of the stock keeps its bin.
func TestShopStockAndPersonalLayer(t *testing.T) {
	a, lb, get := newShopApp(t)
	dir := t.TempDir()
	baseStock(t, dir)
	a.openShop(dir)
	lb.chart("this_month", [3]string{shopID(1), "Kind of Blue", "Miles Davis"}, [3]string{shopID(2), "To Pimp a Butterfly", "Kendrick Lamar"})
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	_, got, _ := get()
	byTitle := map[string]ShopRecord{}
	for _, r := range got.Records {
		byTitle[r.Title] = r
	}
	if _, ok := byTitle["Histoire de Melody Nelson"]; ok || len(got.Records) != 5 {
		t.Errorf("le top de tous les temps est encore lu : %v", recTitles(got.Records))
	}
	if r := byTitle["Kind of Blue"]; r.Month != 1 || r.Style != "modal" {
		t.Errorf("kind of blue : %+v", r)
	}
	if r := byTitle["To Pimp a Butterfly"]; r.Month != 2 || r.Bin != "hiphop" {
		t.Errorf("to pimp a butterfly : %+v", r)
	}
	if r := byTitle["Yeezus"]; r.Bin != "hiphop" || r.Cover == "" {
		t.Errorf("yeezus : %+v", r)
	}
}

// The stock is fetched when it is missing or a week old; a bad answer leaves the last one.
func TestFetchStock(t *testing.T) {
	good := t.TempDir()
	baseStock(t, good)
	file, err := os.ReadFile(filepath.Join(good, "stock.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	body := file
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	a := &App{stockURL: srv.URL}
	dir := t.TempDir()
	a.openShop(dir)
	now := time.Now()
	a.fetchStock(ctx, now)
	if calls.Load() != 1 || len(a.baseNow()) != 3 {
		t.Fatalf("%d appels, %d disques", calls.Load(), len(a.baseNow()))
	}
	a.fetchStock(ctx, now.Add(24*time.Hour))
	if calls.Load() != 1 {
		t.Errorf("refait le lendemain : %d appels", calls.Load())
	}
	body = []byte("<html>pas un stock</html>")
	a.fetchStock(ctx, now.Add(8*24*time.Hour))
	kept, _ := os.ReadFile(filepath.Join(dir, "stock.json.gz"))
	if calls.Load() != 2 || len(a.baseNow()) != 3 || !bytes.Equal(kept, file) {
		t.Errorf("une mauvaise réponse remplace le stock : %d appels, %d disques", calls.Load(), len(a.baseNow()))
	}
	b := &App{} // a restart reads the kept file, without the network
	b.openShop(dir)
	if len(b.baseNow()) != 3 {
		t.Errorf("après redémarrage : %d disques", len(b.baseNow()))
	}
}

// The 3D shop loads an index of the racks, then one rack at a time.
func TestShopRacksIndexAndOne(t *testing.T) {
	a, lb, _ := newShopApp(t)
	lb.chart("this_month", [3]string{shopID(2), "To Pimp a Butterfly", "Kendrick Lamar"})
	h := a.Routes()
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	code, idx, _ := getRacks(t, h, "/api/shop/racks?summary=1")
	if code != 200 || len(idx.Racks) < 2 || idx.Racks[1].ID != "jazz" || idx.Racks[1].Crates[0].N != 1 || idx.Racks[1].Crates[0].Items != nil {
		t.Fatalf("index : %d %+v", code, idx.Racks)
	}
	w := call(h, "GET", "/api/shop/racks?rack=hiphop&hide=1", "")
	var rack Rack
	if err := json.Unmarshal(w.Body.Bytes(), &rack); err != nil || w.Code != 200 || rack.ID != "hiphop" || rack.N != 2 || len(rack.Crates) == 0 || rack.Crates[0].Items == nil {
		t.Fatalf("un rayon : %d %v %+v", w.Code, err, rack) // To Pimp a Butterfly, of the month, and Yeezus
	}
	w = call(h, "GET", "/api/shop/racks?rack=mois", "")
	if err := json.Unmarshal(w.Body.Bytes(), &rack); err != nil || rack.ID != "mois" || rack.N != 1 {
		t.Errorf("ce mois-ci : %d %v %+v", w.Code, err, rack)
	}
	if w := call(h, "GET", "/api/shop/racks?rack=nope", ""); w.Code != 404 || errCode(w) != "shop.noRack" {
		t.Errorf("rayon inconnu : %d %q", w.Code, errMsg(w))
	}
}

// The background pass looks up the Japanese pressings of the personal layer and of the start of the
// stock only: the rest waits for a page to show it.
func TestWarmListSparesTheDeepStock(t *testing.T) {
	old := stockWarm
	stockWarm = 1
	t.Cleanup(func() { stockWarm = old })
	a, lb, _ := newShopApp(t)
	dir := t.TempDir()
	baseStock(t, dir)
	a.openShop(dir)
	lb.chart("this_month", [3]string{shopID(2), "To Pimp a Butterfly", "Kendrick Lamar"})
	if err := a.buildShop(ctx); err != nil {
		t.Fatal(err)
	}
	// The user's artists (Late Registration is also in the stock) and this month's chart first, then the
	// stock's most listened
	if got := recTitles(a.warmList()); !slices.Equal(got, []string{"Late Registration", "Yeezus", "To Pimp a Butterfly", "Waltz for Debby"}) {
		t.Errorf("préchargés : %v", got)
	}
}
