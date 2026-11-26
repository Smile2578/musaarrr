package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscogsBin(t *testing.T) {
	cases := []struct {
		styles, genres []string
		bin, style     string
		ok             bool
	}{
		{[]string{"Hard Bop"}, []string{"Jazz"}, "jazz", "hard bop", true},
		{[]string{"Conscious"}, []string{"Hip Hop"}, "hiphop", "conscious", true}, // the style says nothing, the genre files it
		{nil, []string{"Folk, World, & Country"}, "monde", "", true},
		{[]string{"Country"}, []string{"Folk, World, & Country"}, "country", "country", true},
		{nil, []string{"Stage & Screen"}, "bo", "", true},
		{[]string{"Bossa Nova"}, []string{"Jazz", "Latin"}, "latin", "bossa nova", true}, // the style first
		{[]string{"Marches"}, []string{"Brass & Military"}, "divers", "marches", true},
		{nil, []string{"Non-Music"}, "", "", false},
		{[]string{"Field Recording"}, []string{"Non-Music", "Electronic"}, "", "", false},
		{nil, []string{"Children's"}, "", "", false},
		{nil, nil, "", "", false},
	}
	for _, c := range cases {
		bin, style, ok := discogsBin(c.styles, c.genres)
		if bin != c.bin || style != c.style || ok != c.ok {
			t.Errorf("discogsBin(%q, %q) = %q, %q, %v ; attendu %q, %q, %v", c.styles, c.genres, bin, style, ok, c.bin, c.style, c.ok)
		}
	}
}

func TestReadMasters(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?><masters>
<master id="1"><main_release>10</main_release><images><image height="600" type="primary" uri="" uri150="" width="600"/></images>
 <artists><artist><id>23755</id><name>Miles Davis</name><anv></anv><join></join><role></role><tracks></tracks></artist></artists>
 <genres><genre>Jazz</genre></genres><styles><style>Modal</style><style>Cool Jazz</style></styles><year>1959</year><title>Kind Of Blue</title>
 <data_quality>Correct</data_quality><videos><video duration="300" embed="true" src="https://www.youtube.com/watch?v=x"><title>So What</title><description>d</description></video></videos></master>
<master id="2"><main_release>20</main_release><genres><genre>Funk / Soul</genre></genres><styles><style>Disco</style></styles><title>Sans année</title></master>
<master id="3"><main_release>30</main_release><genres><genre>Rock</genre></genres><year>1970</year><title>Pas voulu</title></master>
</masters>`
	got, err := readMasters(strings.NewReader(xml), map[int]bool{1: true, 2: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]master{1: {"jazz", "modal", 1959}, 2: {"funk", "disco", 0}}
	if len(got) != len(want) {
		t.Fatalf("readMasters : %v, attendu %v", got, want)
	}
	for id, m := range want {
		if got[id] != m {
			t.Errorf("master %d : %+v, attendu %+v", id, got[id], m)
		}
	}
}

// mbLine is a release group as the MusicBrainz JSON dump writes it, one per line.
// mbLine is a line of the dump; link is a Discogs master (with a Wikidata item beside it) or a Wikidata item.
func mbLine(id, title, primary string, secondary []string, date string, credit string, link string) string {
	sec, _ := json.Marshal(secondary)
	rels := "[]"
	switch {
	case strings.Contains(link, "discogs"):
		rels = `[{"type":"discogs","target-type":"url","direction":"forward","url":{"id":"u1","resource":"` + link + `"}},` +
			`{"type":"wikidata","target-type":"url","url":{"resource":"https://www.wikidata.org/wiki/Q1"}}]`
	case link != "":
		rels = `[{"type":"wikidata","target-type":"url","url":{"resource":"` + link + `"}}]`
	}
	return `{"id":"` + id + `","title":"` + title + `","primary-type":"` + primary + `","secondary-types":` + string(sec) +
		`,"first-release-date":"` + date + `","artist-credit":` + credit + `,"relations":` + rels +
		`,"genres":[{"name":"jazz","count":9}],"tags":[{"name":"jazz","count":9}],"rating":{"value":4.5,"votes-count":3}}`
}

func credit(names ...string) string { // name, artist id, sort name, join phrase, repeated
	var parts []string
	for i := 0; i+3 < len(names); i += 4 {
		parts = append(parts, `{"name":"`+names[i]+`","joinphrase":"`+names[i+3]+`","artist":{"id":"`+names[i+1]+`","name":"`+names[i]+`","sort-name":"`+names[i+2]+`"}}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestReadGroups(t *testing.T) {
	miles := credit("Miles Davis", shopID(901), "Davis, Miles", "")
	lines := []string{
		mbLine(shopID(1), "Kind of Blue", "Album", nil, "1959-08-17", miles, "https://www.discogs.com/master/5460"),
		mbLine(shopID(2), "The Best of Miles", "Album", []string{"Compilation"}, "1990", miles, "https://www.discogs.com/master/2"),
		mbLine(shopID(3), "So What", "Single", nil, "1959", miles, "https://www.discogs.com/master/3"),
		mbLine(shopID(4), "Sans lien", "Album", nil, "1960", miles, ""),
		mbLine(shopID(5), "Live at the Plugged Nickel", "Album", []string{"Live"}, "1965-12", miles, "https://www.discogs.com/master/55"),
		mbLine(shopID(6), "Brasilian Skies", "Album", nil, "1978", credit("高中正義", shopID(902), "Takanaka, Masayoshi", ""), "http://discogs.com/master/66"),
		mbLine(shopID(7), "Duo", "Album", nil, "", credit("Ella Fitzgerald", shopID(903), "Fitzgerald, Ella", " & ", "Louis Armstrong", shopID(904), "Armstrong, Louis", ""), "https://www.discogs.com/fr/master/77-Ella-Louis"),
		mbLine(shopID(8), "Pas un master", "Album", nil, "1980", miles, "https://www.discogs.com/release/8"), // its Wikidata item stays
		mbLine(shopID(10), "Wikidata seul", "Album", nil, "1981", miles, "https://www.wikidata.org/wiki/Q10"),
		mbLine(shopID(11), "Pas un élément", "Album", nil, "1982", miles, "https://www.wikidata.org/wiki/Property:P136"),
		`{"id":"` + shopID(9) + `","title":"Coupée","primary-type":"Album","relations":[{"url":{"resource":"https://www.discogs.com/master/9"`,
	}
	cands, bad, err := readGroups(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if bad != 1 {
		t.Errorf("lignes illisibles : %d, attendu 1", bad)
	}
	type row struct {
		mbid, title, artist, artistMBID, sort string
		year, master                          int
		wd                                    string
	}
	var got []row
	for _, c := range cands {
		got = append(got, row{c.rec.MBID, c.rec.Title, c.rec.Artist, c.rec.ArtistMBID, c.rec.Sort, c.rec.Year, c.master, c.wd})
	}
	want := []row{
		{shopID(1), "Kind of Blue", "Miles Davis", shopID(901), "", 1959, 5460, "Q1"},
		{shopID(5), "Live at the Plugged Nickel", "Miles Davis", shopID(901), "", 1965, 55, "Q1"},
		{shopID(6), "Brasilian Skies", "高中正義", shopID(902), "Takanaka, Masayoshi", 1978, 66, "Q1"},
		{shopID(7), "Duo", "Ella Fitzgerald & Louis Armstrong", shopID(903), "", 0, 77, "Q1"},
		{shopID(8), "Pas un master", "Miles Davis", shopID(901), "", 1980, 0, "Q1"},
		{shopID(10), "Wikidata seul", "Miles Davis", shopID(901), "", 1981, 0, "Q10"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("readGroups :\n%+v\nattendu :\n%+v", got, want)
	}
}

func TestShare(t *testing.T) {
	cases := []struct {
		total int
		w     []float64
		caps  []int
		want  []int
	}{
		{10, []float64{1, 1}, []int{3, 100}, []int{3, 7}}, // what a bin cannot fill goes to the others
		{5, []float64{2, 1, 0}, []int{9, 9, 9}, []int{3, 2, 0}},
		{100, []float64{1}, []int{4}, []int{4}},
		{0, []float64{1, 1}, []int{5, 5}, []int{0, 0}},
	}
	for _, c := range cases {
		if got := share(c.total, c.w, c.caps); !slices.Equal(got, c.want) {
			t.Errorf("share(%d, %v, %v) = %v, attendu %v", c.total, c.w, c.caps, got, c.want)
		}
	}
}

// cand is a candidate of the stock: an album of bin by artist, released on date, with users listeners.
func cand(i int, bin, artist, date string, users int64) stockCand {
	return stockCand{rec: ShopRecord{MBID: shopID(i), Title: fmt.Sprint("T", i), Artist: artist, ArtistMBID: artist, Year: yearOf(date), Bin: bin},
		users: users, date: date}
}

func TestPickStockBalance(t *testing.T) {
	var cands []stockCand
	for i := range 50 {
		cands = append(cands, cand(i, "rock", fmt.Sprint("A", i), "1965", 100), cand(100+i, "rock", fmt.Sprint("B", i), "2015", 1000))
	}
	for i := range 5 {
		cands = append(cands, cand(200+i, "jazz", fmt.Sprint("C", i), "1959", 50))
	}
	cands = append(cands, cand(300, "jazz", "D", "1959", 4)) // under the threshold of 5 listeners
	got := pickStock(cands, 40, map[string]float64{"rock": 1, "jazz": 1}, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC))
	n := map[string]int{}
	for _, c := range got {
		n[c.rec.Bin+fmt.Sprint(c.rec.Year/10*10)]++
	}
	if len(got) != 40 || n["jazz1950"] != 5 || n["rock1960"] < 7 || n["rock1960"] > 10 {
		t.Errorf("pickStock : %d disques, %v", len(got), n)
	}
}

func TestPickStockLayers(t *testing.T) {
	now := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	var cands []stockCand
	for i := range 10 { // Miles's albums: 1 000 listeners for the first, 100 for the tenth
		cands = append(cands, cand(i, "jazz", "Miles", "1965", int64(1000-100*i)))
	}
	for i := range 60 {
		cands = append(cands, cand(100+i, "jazz", fmt.Sprint("A", i), "1965", 650))
	}
	cands = append(cands, cand(200, "jazz", "New", "2026-10-30", 6), cand(201, "jazz", "Old", "2026-07-01", 6))
	got := pickStock(cands, 30, map[string]float64{"jazz": 1}, now)
	layer := map[string]string{}
	var deep []string
	for _, c := range got {
		layer[c.rec.MBID] = c.rec.Layer
		if c.rec.Layer == "deep" {
			deep = append(deep, c.rec.MBID)
		}
	}
	if len(got) != 30 || layer[shopID(200)] != "new" || layer[shopID(201)] != "" {
		t.Errorf("nouveautés : %d disques, %q %q", len(got), layer[shopID(200)], layer[shopID(201)])
	}
	// the canon has Miles's four most listened; his 5th to 8th come back as deep cuts
	if want := []string{shopID(4), shopID(5), shopID(6), shopID(7)}; !slices.Equal(deep, want) || layer[shopID(0)] != "" {
		t.Errorf("pépites : %v, attendu %v", deep, want)
	}
}

func TestPickStockPerArtist(t *testing.T) {
	var cands []stockCand
	for i := range 30 {
		cands = append(cands, cand(i, "jazz", "Miles", fmt.Sprint(1950+i), 1000))
	}
	if got := pickStock(cands, 100, map[string]float64{"jazz": 1}, time.Now()); len(got) != stockPerArtist {
		t.Errorf("%d albums du même artiste, %d au plus", len(got), stockPerArtist)
	}
}

func TestPickStockDeepCutsSkipTheTopTwo(t *testing.T) {
	var cands []stockCand
	for i := range 20 { // a crowded 1950s: Miles's two most listened do not make its canon
		cands = append(cands, cand(i, "jazz", fmt.Sprint("A", i), "1955", 5000))
	}
	cands = append(cands, cand(50, "jazz", "Miles", "1955", 1000), cand(51, "jazz", "Miles", "1956", 900))
	for i := range 8 { // his 3rd and 4th lead the 1960s, his 5th to 8th follow the others
		users := []int64{800, 700, 300, 200, 100, 90, 80, 70}[i]
		cands = append(cands, cand(60+i, "jazz", "Miles", "1965", users))
	}
	for i := range 20 {
		cands = append(cands, cand(100+i, "jazz", fmt.Sprint("B", i), "1965", 400))
	}
	got := pickStock(cands, 20, map[string]float64{"jazz": 1}, time.Now())
	for _, c := range got {
		if c.rec.Layer == "deep" && (c.rec.MBID == shopID(50) || c.rec.MBID == shopID(51)) {
			t.Errorf("le 1er ou 2e album d'un artiste n'est pas une pépite : %+v", c.rec)
		}
	}
}

// lbStock answers like ListenBrainz's popularity and metadata, for the albums of users.
func lbStock(t *testing.T, users map[string]int64, covers map[string]int64) *Listens {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/1/popularity/release-group":
			var body struct {
				MBIDs []string `json:"release_group_mbids"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			var rows []string
			for _, id := range body.MBIDs {
				rows = append(rows, fmt.Sprintf(`{"release_group_mbid":%q,"total_user_count":%d,"total_listen_count":%d}`, id, users[id], users[id]*10))
			}
			fmt.Fprint(w, "["+strings.Join(rows, ",")+"]")
		case "/1/metadata/release_group/":
			var rows []string
			for _, id := range strings.Split(r.URL.Query().Get("release_group_mbids"), ",") {
				if caa := covers[id]; caa > 0 {
					rows = append(rows, fmt.Sprintf(`%q:{"release_group":{"caa_id":%d,"caa_release_mbid":%q}}`, id, caa, shopID(999)))
				} else {
					rows = append(rows, fmt.Sprintf(`%q:{"release_group":{"caa_id":null,"caa_release_mbid":null}}`, id))
				}
			}
			fmt.Fprint(w, "{"+strings.Join(rows, ",")+"}")
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return NewListens(srv.URL, "")
}

func TestBuildStock(t *testing.T) {
	quickShop(t, 10, 10)
	miles := credit("Miles Davis", shopID(901), "Davis, Miles", "")
	mb := strings.Join([]string{
		mbLine(shopID(1), "Kind of Blue", "Album", nil, "1959-08-17", miles, "https://www.discogs.com/master/1"),
		mbLine(shopID(2), "Sans pochette", "Album", nil, "1960", miles, "https://www.discogs.com/master/2"),
		mbLine(shopID(3), "Brasilian Skies", "Album", nil, "", credit("高中正義", shopID(902), "Takanaka, Masayoshi", ""), "https://www.discogs.com/master/3"),
		mbLine(shopID(4), "Bruit", "Album", nil, "1990", miles, "https://www.discogs.com/master/4"),
		mbLine(shopID(5), "Peu écouté", "Album", nil, "1970", miles, "https://www.discogs.com/master/5"),
	}, "\n")
	discogs := `<masters><master id="1"><genres><genre>Jazz</genre></genres><styles><style>Modal</style></styles><year>1959</year></master>
		<master id="2"><genres><genre>Jazz</genre></genres></master>
		<master id="3"><genres><genre>Pop</genre></genres><styles><style>City Pop</style></styles><year>1978</year></master>
		<master id="4"><genres><genre>Non-Music</genre></genres></master>
		<master id="5"><genres><genre>Jazz</genre></genres></master></masters>`
	l := lbStock(t, map[string]int64{shopID(1): 900, shopID(2): 800, shopID(3): 700, shopID(4): 600, shopID(5): 4},
		map[string]int64{shopID(1): 11, shopID(3): 33, shopID(4): 44, shopID(5): 55})
	recs, err := buildStock(context.Background(), strings.NewReader(mb), strings.NewReader(discogs), l, nil, 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range recs {
		got = append(got, fmt.Sprint(r.Title, "/", r.Bin, "/", r.Style, "/", r.Year, "/", r.Listens))
	}
	if want := []string{"Kind of Blue/jazz/modal/1959/9000", "Brasilian Skies/japon/city pop/1978/7000"}; !slices.Equal(got, want) {
		t.Errorf("buildStock : %v, attendu %v", got, want)
	}
	if recs[1].Sort != "Takanaka, Masayoshi" || recs[0].ArtistMBID != shopID(901) {
		t.Errorf("artistes : %+v", recs)
	}
}

func TestStockFileRoundTrip(t *testing.T) {
	built := time.Date(2026, 12, 3, 5, 17, 0, 0, time.UTC)
	recs := []ShopRecord{
		{MBID: shopID(1), Title: "Kind of Blue", Artist: "Miles Davis", ArtistMBID: shopID(901), Year: 1959, Bin: "jazz", Style: "modal", Listens: 9000},
		{MBID: shopID(2), Title: "Nefertiti", Artist: "Miles Davis", ArtistMBID: shopID(901), Year: 1968, Bin: "jazz", Listens: 800, Layer: "deep"},
		{MBID: shopID(3), Title: "Brasilian Skies", Artist: "高中正義", ArtistMBID: shopID(902), Sort: "Takanaka, Masayoshi", Bin: "japon", Layer: "new"},
	}
	path := t.TempDir() + "/stock.json.gz"
	if err := writeStock(path, recs, built, map[string]string{"musicbrainz": "20261125-001001"}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, gotBuilt, err := readStock(f)
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(recs)
	for i := range want {
		want[i].Listens = 0 // the file keeps ListenBrainz's order, not its counts, whose licence is not confirmed
	}
	if !slices.Equal(got, want) || !gotBuilt.Equal(built) {
		t.Errorf("relu :\n%+v\n%v\nattendu :\n%+v", got, gotBuilt, want)
	}
}

// Without a Discogs dump, the albums are filed by their genres on Wikidata, else their performer's.
func TestBuildStockFromWikidata(t *testing.T) {
	quickShop(t, 10, 10)
	miles := credit("Miles Davis", shopID(901), "Davis, Miles", "")
	mb := strings.Join([]string{
		mbLine(shopID(1), "Kind of Blue", "Album", nil, "1959-08-17", miles, "https://www.wikidata.org/wiki/Q11"),
		mbLine(shopID(3), "Brasilian Skies", "Album", nil, "", credit("高中正義", shopID(902), "Takanaka, Masayoshi", ""), "https://www.wikidata.org/wiki/Q33"),
		mbLine(shopID(4), "Sans genre", "Album", nil, "1990", miles, "https://www.wikidata.org/wiki/Q44"),
		mbLine(shopID(5), "Sans lien", "Album", nil, "1970", miles, ""),
	}, "\n")
	l := lbStock(t, map[string]int64{shopID(1): 900, shopID(3): 700, shopID(4): 600, shopID(5): 500},
		map[string]int64{shopID(1): 11, shopID(3): 33, shopID(4): 44, shopID(5): 55})
	w := wdStock(t, map[string][2][]string{"Q11": {{"modal jazz", "cool jazz"}, {"jazz"}}, "Q33": {nil, {"city pop", "J-pop"}}})
	recs, err := buildStock(context.Background(), strings.NewReader(mb), nil, l, w, 10, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range recs {
		got = append(got, fmt.Sprint(r.Title, "/", r.Bin, "/", r.Style, "/", r.Year))
	}
	if want := []string{"Kind of Blue/jazz/modal jazz/1959", "Brasilian Skies/japon/city pop/0"}; !slices.Equal(got, want) {
		t.Errorf("buildStock : %v, attendu %v", got, want)
	}
}

func TestWikiBin(t *testing.T) {
	cases := []struct {
		album, artist []string
		bin, style    string
	}{
		{[]string{"cool jazz", "modal jazz"}, nil, "jazz", "modal jazz"},
		{[]string{"hip hop music", "jazz rap", "funk", "conscious hip hop"}, []string{"jazz"}, "hiphop", "conscious hip hop"},
		{nil, []string{"rock music", "blues rock"}, "rock", "blues rock"},
		{[]string{"funk", "jazz"}, nil, "jazz", "jazz"},             // a tie goes to the first rack of the shop
		{[]string{"art song"}, []string{"pop music"}, "pop", "pop"}, // the performer's genre before « divers »
		{[]string{"art song"}, nil, "divers", "art song"},
		{[]string{"J-pop"}, nil, "japon", "j-pop"},
		{nil, nil, "", ""},
	}
	for _, c := range cases {
		if bin, style := wikiBin(wdGenres{c.album, c.artist}); bin != c.bin || style != c.style {
			t.Errorf("%v %v : %q %q, attendu %q %q", c.album, c.artist, bin, style, c.bin, c.style)
		}
	}
}

// wdStock is a Wikidata Query Service that knows, for each item, its genres and its performer's.
func wdStock(t *testing.T, genres map[string][2][]string) *Wikidata {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// without the hint, the service joins every label on Wikidata before the albums, and times out
		if r.Method != "POST" || r.Header.Get("User-Agent") != lbUserAgent || !strings.Contains(r.Header.Get("Accept"), "sparql-results+json") ||
			!strings.Contains(r.FormValue("query"), `hint:Query hint:optimizer "None"`) {
			w.WriteHeader(400)
			return
		}
		var rows []string
		for _, q := range regexp.MustCompile(`wd:(Q\d+)`).FindAllStringSubmatch(r.FormValue("query"), -1) {
			for k, labels := range genres[q[1]] {
				for _, l := range labels {
					rows = append(rows, fmt.Sprintf(`{"item":{"type":"uri","value":"http://www.wikidata.org/entity/%s"},"kind":{"type":"literal","value":%q},`+
						`"label":{"xml:lang":"en","type":"literal","value":%q}}`, q[1], "ap"[k:k+1], l))
				}
			}
		}
		fmt.Fprint(w, `{"head":{"vars":["item","kind","label"]},"results":{"bindings":[`+strings.Join(rows, ",")+`]}}`)
	}))
	t.Cleanup(srv.Close)
	return NewWikidata(srv.URL)
}

// A busy query service says how long to wait.
func TestWikidataBusy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
	}))
	t.Cleanup(srv.Close)
	_, err := NewWikidata(srv.URL).Genres(context.Background(), []string{"Q1"})
	if busy := (lbBusy{}); !errors.As(err, &busy) || busy.wait != 7*time.Second {
		t.Errorf("%v", err)
	}
}

// A query that keeps failing leaves its albums out; the build goes on unless many fail.
func TestFileByWikidataSkipsAFailedQuery(t *testing.T) {
	quickShop(t, 10, 10)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	cands := []stockCand{{rec: ShopRecord{MBID: shopID(1)}, wd: "Q1"}}
	got, err := fileByWikidata(context.Background(), NewWikidata(srv.URL), cands)
	if err != nil || len(got) != 0 || calls.Load() != 3 {
		t.Errorf("%v %v, %d appels", got, err, calls.Load())
	}
	many := make([]stockCand, 50*wdBatch)
	for i := range many {
		many[i] = stockCand{rec: ShopRecord{MBID: shopID(i)}, wd: fmt.Sprint("Q", i+1)}
	}
	calls.Store(0)
	if _, err := fileByWikidata(context.Background(), NewWikidata(srv.URL), many); err == nil || calls.Load() != 4*3 {
		t.Errorf("every query fails: the build should stop after the fourth, %v after %d calls", err, calls.Load())
	}
}
