package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func shopRec(bin, artist, title string, year int) ShopRecord {
	return ShopRecord{MBID: artist + "/" + title, Title: title, Artist: artist, Year: year, Bin: bin}
}

func noneOwned(ShopRecord) bool { return false }

func crateLabels(cs []Crate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Label)
	}
	return out
}

// crateLine reads a crate as « [Divider:2] Artist/Title … ».
func crateLine(c Crate) string {
	var parts []string
	for _, it := range c.Items {
		if it.Divider != "" {
			parts = append(parts, fmt.Sprintf("[%s:%d]", it.Divider, it.N))
		} else {
			parts = append(parts, it.Record.Artist+"/"+it.Record.Title)
		}
	}
	return strings.Join(parts, " ")
}

func TestBuildRacksByBinAndLetter(t *testing.T) {
	var recs []ShopRecord
	for i := range 26 {
		recs = append(recs, shopRec("jazz", fmt.Sprintf("%c Artist", 'A'+i), "X", 1960))
	}
	recs = append(recs, shopRec("soul", "Al Green", "Let's Stay Together", 1972))
	racks := buildRacks(recs, noneOwned, false)
	if len(racks) != 2 || racks[0].ID != "jazz" || racks[0].Name != "Jazz" || racks[0].N != 26 || racks[1].ID != "soul" || racks[1].Name != "Soul et R&B" {
		t.Fatalf("racks %+v", racks)
	}
	if got := crateLabels(racks[0].Crates); !slices.Equal(got, []string{"A–L", "M–X", "Y–Z"}) {
		t.Fatalf("labels %q", got)
	}
	for _, c := range racks[0].Crates {
		if c.N != len(c.Items) || c.N > crateMax {
			t.Errorf("crate %s: n %d, %d items", c.Label, c.N, len(c.Items))
		}
	}
}

func TestBuildRacksNeverSplitsAnArtist(t *testing.T) {
	var recs []ShopRecord
	for i := range 5 {
		recs = append(recs, shopRec("jazz", fmt.Sprintf("Aa%d", i), "X", 1960))
	}
	for i := range 10 {
		recs = append(recs, shopRec("jazz", "Al Jarreau", fmt.Sprintf("Album %02d", i), 1970+i))
	}
	cs := buildRacks(recs, noneOwned, false)[0].Crates
	if len(cs) != 2 || cs[0].N != 5 || cs[1].N != 10 || cs[1].Items[0].Divider != "Al Jarreau" || cs[1].Items[0].N != 10 {
		t.Fatalf("crates %q / %q", crateLine(cs[0]), crateLine(cs[len(cs)-1]))
	}
}

func TestBuildRacksSplitsAHugeArtist(t *testing.T) {
	var recs []ShopRecord
	for i := range 15 {
		recs = append(recs, shopRec("jazz", "Miles Davis", fmt.Sprintf("Album %02d", i), 1950+i))
	}
	cs := buildRacks(recs, noneOwned, false)[0].Crates
	if got := crateLabels(cs); !slices.Equal(got, []string{"Mi", "Mi"}) {
		t.Fatalf("labels %q", got)
	}
	if cs[0].N != 12 || cs[1].N != 3 || cs[0].Items[0].Divider != "Miles Davis" || cs[0].Items[0].N != 12 || cs[1].Items[0].N != 3 {
		t.Fatalf("crates %q / %q", crateLine(cs[0]), crateLine(cs[1]))
	}
}

func TestBuildRacksDividers(t *testing.T) {
	recs := []ShopRecord{
		shopRec("jazz", "John Coltrane", "Giant Steps", 1960),
		shopRec("jazz", "Herbie Hancock", "Maiden Voyage", 1965),
		shopRec("jazz", "John Coltrane", "A Love Supreme", 1965),
	}
	cs := buildRacks(recs, noneOwned, false)[0].Crates
	if got := crateLine(cs[0]); got != "Herbie Hancock/Maiden Voyage [John Coltrane:2] John Coltrane/Giant Steps John Coltrane/A Love Supreme" {
		t.Fatalf("got %q", got)
	}
	if cs[0].Label != "H–J" || cs[0].N != 3 {
		t.Fatalf("crate %+v", cs[0])
	}
}

func TestBuildRacksHideOwned(t *testing.T) {
	recs := []ShopRecord{
		shopRec("jazz", "Bill Evans", "Waltz for Debby", 1962),
		shopRec("jazz", "Ryo Fukui", "Scenery", 1976),
		shopRec("jazz", "Miles Davis", "Kind of Blue", 1959),
		shopRec("soul", "Al Green", "Let's Stay Together", 1972),
	}
	have := func(r ShopRecord) bool { return r.Title != "Kind of Blue" }
	hidden := buildRacks(recs, have, true)
	if len(hidden) != 1 || hidden[0].N != 1 || hidden[0].Crates[0].Items[0].Record.Title != "Kind of Blue" {
		t.Fatalf("hidden %+v", hidden)
	}
	shown := buildRacks(recs, have, false)
	if len(shown) != 2 || shown[0].N != 3 {
		t.Fatalf("shown %+v", shown)
	}
	for _, it := range shown[0].Crates[0].Items {
		if it.Owned != (it.Record.Title != "Kind of Blue") {
			t.Errorf("%s owned %v", it.Record.Title, it.Owned)
		}
	}
}

func TestBuildRacksOddFirstLetters(t *testing.T) {
	recs := []ShopRecord{
		shopRec("rock", "50 Cent", "Get Rich", 2003),
		shopRec("rock", "!!!", "Myth Takes", 2007),
		shopRec("rock", "福居良", "Scenery", 1976),
		shopRec("rock", "The Beatles", "Abbey Road", 1969),
		shopRec("rock", "Édith Piaf", "La Vie en rose", 1947),
		shopRec("rock", "Bill Evans", "Waltz for Debby", 1962),
	}
	cs := buildRacks(recs, noneOwned, false)[0].Crates
	if got := crateLabels(cs); !slices.Equal(got, []string{"B–E", "#"}) {
		t.Fatalf("labels %q", got)
	}
	if got := crateLine(cs[0]); got != "The Beatles/Abbey Road Bill Evans/Waltz for Debby Édith Piaf/La Vie en rose" {
		t.Fatalf("got %q", got)
	}
	if cs[1].N != 3 {
		t.Fatalf("# crate %q", crateLine(cs[1]))
	}
}

// Crates that share one letter are told apart by their first two: « Ha », « He », « Ho ».
func TestBuildRacksTwoLetterLabels(t *testing.T) {
	recs := []ShopRecord{shopRec("jazz", "Hank Mobley", "Soul Station", 1960), shopRec("jazz", "Horace Silver", "Song for My Father", 1965),
		shopRec("jazz", "Horace Silver", "Blowin' the Blues Away", 1959), shopRec("jazz", "Ahmad Jamal", "At the Pershing", 1958)}
	for i := range 12 {
		recs = append(recs, shopRec("jazz", "Herbie Hancock", fmt.Sprintf("Album %02d", i), 1962+i))
	}
	if got := crateLabels(buildRacks(recs, noneOwned, false)[0].Crates); !slices.Equal(got, []string{"A–H", "He", "Ho"}) {
		t.Fatalf("labels %q", got)
	}
}

// An artist written in another script is filed by the sort name MusicBrainz gives (Takanaka under T).
func TestBuildRacksSortName(t *testing.T) {
	taka := shopRec("jazz", "高中正義", "The Rainbow Goblins", 1981)
	taka.Sort = "Takanaka, Masayoshi"
	recs := []ShopRecord{shopRec("jazz", "Sonny Rollins", "Saxophone Colossus", 1956), taka, shopRec("jazz", "Wes Montgomery", "Full House", 1962)}
	cs := buildRacks(recs, noneOwned, false)[0].Crates
	if got := crateLine(cs[0]); len(cs) != 1 || got != "Sonny Rollins/Saxophone Colossus 高中正義/The Rainbow Goblins Wes Montgomery/Full House" || cs[0].Label != "S–W" {
		t.Fatalf("%d crates, %q %q", len(cs), cs[0].Label, got)
	}
}

// The month's rack is filed by rank, twelve to a crate labelled by rank, without dividers, and
// leaves out what the user has when they hide it.
func TestBuildRacksMonth(t *testing.T) {
	var recs []ShopRecord
	for i := 14; i >= 1; i-- {
		r := shopRec([]string{"jazz", "rock"}[i%2], "Ye", fmt.Sprintf("N%d", i), 2000+i)
		r.Month = i
		recs = append(recs, r)
	}
	recs = append(recs, shopRec("jazz", "Bill Evans", "Waltz for Debby", 1962))
	racks := buildRacks(recs, noneOwned, false)
	if ids := []string{racks[0].ID, racks[1].ID, racks[2].ID}; !slices.Equal(ids, []string{"mois", "jazz", "rock"}) {
		t.Fatalf("racks %v", ids)
	}
	m := racks[0]
	if m.N != 14 || !slices.Equal(crateLabels(m.Crates), []string{"1–12", "13–14"}) {
		t.Fatalf("%d records, labels %v", m.N, crateLabels(m.Crates))
	}
	if line := crateLine(m.Crates[1]); line != "Ye/N13 Ye/N14" {
		t.Errorf("second crate %q", line)
	}
	if racks[1].N != 8 {
		t.Errorf("jazz holds %d records", racks[1].N)
	}
	have := func(r ShopRecord) bool { return r.Title == "N1" }
	if hidden := buildRacks(recs, have, true)[0]; hidden.N != 13 || crateLine(hidden.Crates[0])[:6] != "Ye/N2 " || crateLabels(hidden.Crates)[1] != "13" {
		t.Errorf("hidden: %d, %q, %v", hidden.N, crateLine(hidden.Crates[0]), crateLabels(hidden.Crates))
	}
	shown := buildRacks(recs, have, false)[0]
	if !shown.Crates[0].Items[0].Owned || shown.Crates[0].Items[1].Owned {
		t.Errorf("owned marks %+v", shown.Crates[0].Items[:2])
	}
}

// The racks of a full stock are built on each page load: about 50 ms here for 40,000 records.

func BenchmarkBuildRacks40k(b *testing.B) {
	recs := make([]ShopRecord, 40000)
	for i := range recs {
		recs[i] = ShopRecord{MBID: shopID(i), Title: fmt.Sprintf("Album %d", i), Artist: fmt.Sprintf("The Ärtist %d", i/3), Year: 1950 + i%70, Bin: shopBins[i%len(shopBins)]}
	}
	for b.Loop() {
		buildRacks(recs, func(ShopRecord) bool { return false }, false)
	}
}
