package main

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rack is one genre of the shop, as a row of crates along the wall.
type Rack struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	N      int     `json:"n"`
	Crates []Crate `json:"crates"`
}

// Crate holds a run of letters: « A–C », « M », or « # » for names that start with no letter.
type Crate struct {
	Label string     `json:"label"`
	N     int        `json:"n"`
	Items []RackItem `json:"items,omitempty"` // none in the index of the racks
}

// RackItem is a record, or the divider standing before an artist's records in the crate.
type RackItem struct {
	Divider string      `json:"divider,omitempty"`
	N       int         `json:"n,omitempty"`
	Record  *ShopRecord `json:"record,omitempty"`
	Owned   bool        `json:"owned,omitempty"`
	JP      bool        `json:"jp,omitempty"`    // a Japanese pressing's front is known: /api/shop/{mbid}/cover?jp=1
	Color   string      `json:"color,omitempty"` // the shown cover's dominant colour, once Musaarr has read it: "#1e3c8a"
	Thumb   string      `json:"thumb,omitempty"` // its ThumbHash, in base64
}

// crateMax is how many records a crate holds; an artist with more fills several crates.
const crateMax = 12

var binNames = map[string]string{
	"jazz": "Jazz", "soul": "Soul et R&B", "funk": "Funk et disco", "hiphop": "Hip-hop", "rock": "Rock", "indie": "Indé et alternatif",
	"pop": "Pop", "japon": "Japon", "electro": "Électro", "ambient": "Ambient et expérimental", "chanson": "Chanson",
	"classique": "Classique", "reggae": "Reggae et dub", "afro": "Afrique et Antilles", "latin": "Latin et Brésil",
	"monde": "Musiques du monde", "blues": "Blues", "folk": "Folk", "country": "Country", "metal": "Metal", "punk": "Punk et hardcore",
	"bo": "Bandes originales", "divers": "Divers",
}

// rackKey files an artist the way a record shop does: no « The », no accents.
func rackKey(artist string) string {
	return strings.TrimPrefix(strings.TrimSpace(fold(artist)), "the ")
}

// recKey files a record under its artist, by the sort name when the name is in another script.
func recKey(r ShopRecord) string {
	if r.key != "" {
		return r.key
	}
	return rackKey(cmp.Or(r.Sort, r.Artist))
}

// rackLetter is the divider letter of a key: « # » for « 50 Cent », « !!! » or an unsorted « 福居良 ».
func rackLetter(key string) string {
	if key != "" && key[0] >= 'a' && key[0] <= 'z' {
		return strings.ToUpper(key[:1])
	}
	return "#"
}

// otherScript says a name starts with a letter that is not Latin (« 高中正義 », « Кино »): its sort name files it.
func otherScript(name string) bool {
	r, _ := utf8.DecodeRuneInString(rackKey(name))
	return unicode.IsLetter(r) && rackLetter(string(r)) == "#"
}

// twoLetters is the start of a key for a crate label: « He » for « herbie hancock », « J » for « j dilla ».
func twoLetters(key string) string {
	rs := []rune(key)
	if len(rs) > 1 && unicode.IsLetter(rs[1]) {
		return strings.ToUpper(string(rs[0])) + string(rs[1])
	}
	return strings.ToUpper(string(rs[:1]))
}

// buildRacks files the shop's records in racks: first « Ce mois-ci », this month's chart in its order,
// then the genres in the order of shopBins: records by artist then year, crates of crateMax records at
// most that never split an artist unless he has more, a divider before every artist with two records
// or more in the crate, and the « # » records in crates of their own.
func buildRacks(recs []ShopRecord, owned func(ShopRecord) bool, hideOwned bool) []Rack {
	byBin := map[string][]ShopRecord{}
	var month []ShopRecord
	for _, r := range recs {
		if hideOwned && owned(r) {
			continue
		}
		r.key = recKey(r) // the sort below asks for it many times
		byBin[r.Bin] = append(byBin[r.Bin], r)
		if r.Month > 0 {
			month = append(month, r)
		}
	}
	var out []Rack
	if len(month) > 0 {
		slices.SortStableFunc(month, func(a, b ShopRecord) int { return cmp.Compare(a.Month, b.Month) })
		out = append(out, Rack{ID: "mois", Name: "Ce mois-ci", N: len(month), Crates: rankCrates(month, owned, hideOwned)})
	}
	for _, id := range shopBins {
		list := byBin[id]
		if len(list) == 0 {
			continue
		}
		slices.SortStableFunc(list, func(a, b ShopRecord) int {
			ka, kb := recKey(a), recKey(b)
			return cmp.Or(cmp.Compare(btoi(rackLetter(ka) == "#"), btoi(rackLetter(kb) == "#")),
				cmp.Compare(ka, kb), cmp.Compare(a.Year, b.Year), cmp.Compare(a.Title, b.Title))
		})
		out = append(out, Rack{ID: id, Name: binNames[id], N: len(list), Crates: fillCrates(list, owned, hideOwned)})
	}
	return out
}

// rankCrates packs a chart into crates of crateMax, labelled by place: « 1–12 », « 13–24 », « 25 ».
func rankCrates(list []ShopRecord, owned func(ShopRecord) bool, hideOwned bool) []Crate {
	var crates []Crate
	for i := 0; i < len(list); i += crateMax {
		run := list[i:min(i+crateMax, len(list))]
		c := Crate{N: len(run), Label: strconv.Itoa(i + 1)}
		if len(run) > 1 {
			c.Label += "–" + strconv.Itoa(i+len(run))
		}
		for _, r := range run {
			c.Items = append(c.Items, RackItem{Record: &r, Owned: !hideOwned && owned(r)})
		}
		crates = append(crates, c)
	}
	return crates
}

// fillCrates packs sorted records, one artist's run at a time, into crates.
func fillCrates(list []ShopRecord, owned func(ShopRecord) bool, hideOwned bool) []Crate {
	var runs [][]ShopRecord // one artist's records, cut at crateMax
	for i := 0; i < len(list); {
		j := i
		for j < len(list) && recKey(list[j]) == recKey(list[i]) {
			j++
		}
		for k := i; k < j; k += crateMax {
			runs = append(runs, list[k:min(k+crateMax, j)])
		}
		i = j
	}
	var crates []Crate
	var ends [][2]string // each crate's first and last keys, for its label
	var cur [][]ShopRecord
	n := 0
	flush := func() {
		if n == 0 {
			return
		}
		c := Crate{N: n}
		first, last := recKey(cur[0][0]), recKey(cur[len(cur)-1][0])
		ends = append(ends, [2]string{first, last})
		c.Label = rackLetter(first)
		if rackLetter(last) != c.Label {
			c.Label += "–" + rackLetter(last)
		}
		for _, run := range cur {
			if len(run) >= 2 {
				c.Items = append(c.Items, RackItem{Divider: run[0].Artist, N: len(run)})
			}
			for _, r := range run {
				c.Items = append(c.Items, RackItem{Record: &r, Owned: !hideOwned && owned(r)})
			}
		}
		crates = append(crates, c)
		cur, n = nil, 0
	}
	for _, run := range runs {
		hash := rackLetter(recKey(run[0])) == "#"
		if n > 0 && (n+len(run) > crateMax || hash != (rackLetter(recKey(cur[0][0])) == "#")) {
			flush()
		}
		cur = append(cur, run)
		n += len(run)
	}
	flush()
	// a letter that fills several crates labels each by its first two: « He », « Ho–Hu »
	labels := make([]string, len(crates))
	for i, c := range crates {
		labels[i] = c.Label
	}
	for i, l := range labels {
		same := func(j int) bool { return j >= 0 && j < len(labels) && labels[j] == l }
		if len(l) != 1 || l == "#" || !(same(i-1) || same(i+1)) {
			continue
		}
		crates[i].Label = twoLetters(ends[i][0])
		if last := twoLetters(ends[i][1]); last != crates[i].Label {
			crates[i].Label += "–" + last
		}
	}
	return crates
}
