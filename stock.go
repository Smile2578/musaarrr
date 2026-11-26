package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The shop's starting stock is built once a month by the CI (.github/workflows/stock.yml) from open
// data only, then fetched by each install: MusicBrainz's albums and their Discogs master (CC0), the
// Discogs master's styles for the bin (CC0), and ListenBrainz's listeners for the ranking.

// master is what the stock keeps of a Discogs master: its bin, the style shown, its year.
type master struct {
	bin, style string
	year       int
}

// discogsBin files a Discogs master by its first style that names a bin, else by its first genre
// that does; the style shown is that style, else the master's first. Records that are not music,
// or are for children, are left out.
func discogsBin(styles, genres []string) (bin, style string, ok bool) {
	for _, g := range genres {
		if g == "Non-Music" || g == "Children's" {
			return "", "", false
		}
	}
	if len(styles) == 0 && len(genres) == 0 {
		return "", "", false
	}
	for _, s := range styles {
		if b := genreBin(s); b != "" {
			return b, strings.ToLower(s), true
		}
	}
	if len(styles) > 0 {
		style = strings.ToLower(styles[0])
	}
	for _, g := range genres {
		if b := genreBin(g); b != "" {
			return b, style, true
		}
	}
	return "divers", style, true
}

// readMasters reads a Discogs masters dump and keeps the wanted masters that are music.
func readMasters(r io.Reader, want map[int]bool) (map[int]master, error) {
	d := xml.NewDecoder(r)
	out := map[int]master{}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "master" {
			continue
		}
		id := 0
		for _, a := range se.Attr {
			if a.Name.Local == "id" {
				id, _ = strconv.Atoi(a.Value)
			}
		}
		if !want[id] {
			if err := d.Skip(); err != nil {
				return out, err
			}
			continue
		}
		var m struct {
			Genres []string `xml:"genres>genre"`
			Styles []string `xml:"styles>style"`
			Year   int      `xml:"year"`
		}
		if err := d.DecodeElement(&m, &se); err != nil {
			return out, err
		}
		if bin, style, ok := discogsBin(m.Styles, m.Genres); ok {
			out[id] = master{bin, style, m.Year}
		}
	}
}

// stockCand is an album that may enter the stock: its record, its Discogs master, its listeners.
type stockCand struct {
	rec    ShopRecord
	master int    // its Discogs master
	wd     string // its Wikidata item: "Q123"
	users  int64
	date   string // first release, as MusicBrainz writes it: "1959-08-17", "1959-08" or "1959"
}

// notInShop are the secondary types of the albums a record shop does not file with the others.
// Live and Soundtrack stay.
var notInShop = map[string]bool{"Compilation": true, "DJ-mix": true, "Mixtape/Street": true, "Remix": true, "Demo": true,
	"Spokenword": true, "Interview": true, "Audiobook": true, "Audio drama": true, "Field recording": true}

var (
	discogsMaster = regexp.MustCompile(`discogs\.com/(?:[a-z]{2}/)?master/(\d+)`)
	wikidataItem  = regexp.MustCompile(`^https?://www\.wikidata\.org/wiki/(Q\d+)$`)
)

// readGroups reads the MusicBrainz JSON dump of release groups, one per line, and keeps the albums
// with a Discogs master or a Wikidata item, which give their genre. Only CC0 fields are read: the dump's genres, tags and ratings are under a
// non-commercial licence and never enter the stock. A line that does not parse is counted in bad.
func readGroups(r io.Reader) (cands []stockCand, bad int, err error) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && (bytes.Contains(line, []byte("/master/")) || bytes.Contains(line, []byte("wikidata.org/wiki/Q"))) {
			if c, ok, parsed := readGroup(line); !parsed {
				bad++
			} else if ok {
				cands = append(cands, c)
			}
		}
		if errors.Is(err, io.EOF) {
			return cands, bad, nil
		}
		if err != nil {
			return cands, bad, err
		}
	}
}

// mbGroup is a line of the MusicBrainz dump of release groups, its CC0 fields only.
type mbGroup struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Primary      string   `json:"primary-type"`
	Secondary    []string `json:"secondary-types"`
	Date         string   `json:"first-release-date"`
	ArtistCredit []struct {
		Name   string `json:"name"`
		Join   string `json:"joinphrase"`
		Artist struct {
			ID   string `json:"id"`
			Sort string `json:"sort-name"`
		} `json:"artist"`
	} `json:"artist-credit"`
	Relations []struct {
		URL struct {
			Resource string `json:"resource"`
		} `json:"url"`
	} `json:"relations"`
}

func readGroup(line []byte) (c stockCand, ok, parsed bool) {
	var g mbGroup
	if err := json.Unmarshal(line, &g); err != nil {
		return c, false, false
	}
	if _, valid := validMBID(g.ID); !valid || g.Primary != "Album" || len(g.ArtistCredit) == 0 {
		return c, false, true
	}
	for _, s := range g.Secondary {
		if notInShop[s] {
			return c, false, true
		}
	}
	for _, rel := range g.Relations {
		if m := discogsMaster.FindStringSubmatch(rel.URL.Resource); m != nil && c.master == 0 {
			c.master, _ = strconv.Atoi(m[1])
		}
		if m := wikidataItem.FindStringSubmatch(rel.URL.Resource); m != nil && c.wd == "" {
			c.wd = m[1]
		}
	}
	if c.master == 0 && c.wd == "" {
		return c, false, true
	}
	var artist strings.Builder
	for _, ac := range g.ArtistCredit {
		artist.WriteString(ac.Name + ac.Join)
	}
	c.rec = ShopRecord{MBID: g.ID, Title: g.Title, Artist: artist.String(), ArtistMBID: g.ArtistCredit[0].Artist.ID, Year: yearOf(g.Date)}
	c.date = g.Date
	if otherScript(c.rec.Artist) {
		c.rec.Sort = g.ArtistCredit[0].Artist.Sort
	}
	return c, true, true
}

// stockWeights are the bins' shares of the stock, relative to each other.
var stockWeights = map[string]float64{"rock": 11, "jazz": 9, "indie": 8, "electro": 7, "hiphop": 7, "soul": 6, "pop": 6,
	"metal": 4, "funk": 4, "punk": 3, "folk": 3, "country": 3, "ambient": 3, "reggae": 3, "latin": 3, "classique": 3,
	"japon": 2, "afro": 2, "monde": 2, "blues": 2, "chanson": 1, "bo": 1, "divers": 0.5}

const (
	stockMinUsers  = 5    // listeners an album needs to enter the stock
	stockPerArtist = 20   // albums of one artist at most
	stockNewShare  = 0.05 // of a bin: its new releases
	stockDeepShare = 0.20 // of a bin: its deep cuts
	stockNewDays   = 90
)

// share splits total in proportion to the weights, each part at most its cap: what a part cannot
// take goes to the others, and the last units to the largest remainders.
func share(total int, w []float64, caps []int) []int {
	out := make([]int, len(w))
	for total > 0 {
		sum := 0.0
		for i := range w {
			if out[i] < caps[i] && w[i] > 0 {
				sum += w[i]
			}
		}
		if sum == 0 {
			break
		}
		type part struct {
			i    int
			frac float64
		}
		var open []part
		capped, given := false, 0
		for i := range w {
			if out[i] >= caps[i] || w[i] <= 0 {
				continue
			}
			exact := float64(total) * w[i] / sum
			k := int(exact)
			if k >= caps[i]-out[i] {
				k, capped = caps[i]-out[i], true
			} else {
				open = append(open, part{i, exact - float64(k)})
			}
			out[i] += k
			given += k
		}
		total -= given
		if capped {
			continue // the parts left share again what the full ones could not take
		}
		slices.SortStableFunc(open, func(a, b part) int { return cmp.Compare(b.frac, a.frac) })
		for _, p := range open[:min(total, len(open))] {
			out[p.i]++
		}
		break
	}
	return out
}

// byListeners orders the candidates most listened first, by listeners then listens.
func byListeners(a, b stockCand) int {
	return cmp.Or(cmp.Compare(b.users, a.users), cmp.Compare(b.rec.Listens, a.rec.Listens), cmp.Compare(a.rec.MBID, b.rec.MBID))
}

func artistKey(r ShopRecord) string { return cmp.Or(r.ArtistMBID, r.Artist) }

// pickStock chooses n albums among the candidates: each bin its share by weight, then in each bin
// its new releases, its canon balanced by decade, and its deep cuts.
func pickStock(cands []stockCand, n int, weights map[string]float64, now time.Time) []stockCand {
	byBin := map[string][]stockCand{}
	for _, c := range cands {
		if c.users >= stockMinUsers && weights[c.rec.Bin] > 0 {
			c.rec.Layer = "" // chosen again: a deep cut of a first choice may be in the canon now
			byBin[c.rec.Bin] = append(byBin[c.rec.Bin], c)
		}
	}
	bins := slices.Sorted(maps.Keys(byBin))
	w, caps := make([]float64, len(bins)), make([]int, len(bins))
	for i, b := range bins {
		list := byBin[b]
		slices.SortFunc(list, byListeners)
		perArtist := map[string]int{}
		list = slices.DeleteFunc(list, func(c stockCand) bool {
			perArtist[artistKey(c.rec)]++
			return perArtist[artistKey(c.rec)] > stockPerArtist
		})
		byBin[b], w[i], caps[i] = list, weights[b], len(list)
	}
	var out []stockCand
	for i, total := range share(n, w, caps) {
		out = append(out, pickBin(byBin[bins[i]], total, now)...)
	}
	return out
}

// pickBin chooses total albums of one bin, most listened first: its new releases, its deep cuts
// among the artists of its canon, and its canon.
func pickBin(list []stockCand, total int, now time.Time) []stockCand {
	quota := func(f float64) int { return int(math.Round(float64(total) * f)) }
	var fresh, rest []stockCand
	for _, c := range list {
		if len(fresh) < quota(stockNewShare) && isNew(c.date, now) {
			c.rec.Layer = "new"
			fresh = append(fresh, c)
		} else {
			rest = append(rest, c)
		}
	}
	canon := byDecade(rest, total-len(fresh)-quota(stockDeepShare))
	deep := deepCuts(rest, canon, quota(stockDeepShare))
	taken := map[string]bool{}
	for _, c := range deep {
		taken[c.rec.MBID] = true
	}
	canon = byDecade(slices.DeleteFunc(slices.Clone(rest), func(c stockCand) bool { return taken[c.rec.MBID] }), total-len(fresh)-len(deep))
	return slices.Concat(fresh, canon, deep)
}

// byDecade takes k of the sorted candidates, each decade its share by the square root of its
// listeners: the decades ListenBrainz's listeners play most lead without crowding out the others.
func byDecade(list []stockCand, k int) []stockCand {
	var decades []int
	byDec := map[int][]stockCand{}
	mass := map[int]float64{}
	for _, c := range list {
		d := c.rec.Year / 10
		if _, ok := byDec[d]; !ok {
			decades = append(decades, d)
		}
		byDec[d] = append(byDec[d], c)
		mass[d] += float64(c.users)
	}
	w, caps := make([]float64, len(decades)), make([]int, len(decades))
	for i, d := range decades {
		w[i], caps[i] = math.Sqrt(mass[d]), len(byDec[d])
	}
	var out []stockCand
	for i, n := range share(max(k, 0), w, caps) {
		out = append(out, byDec[decades[i]][:n]...)
	}
	return out
}

// deepCuts are up to k albums ranked 3rd to 8th by listeners among their artist's, for the artists
// of the canon, the albums of the canon left out.
func deepCuts(list, canon []stockCand, k int) []stockCand {
	inCanon, artists := map[string]bool{}, map[string]bool{}
	for _, c := range canon {
		inCanon[c.rec.MBID], artists[artistKey(c.rec)] = true, true
	}
	rank := map[string]int{}
	var out []stockCand
	for _, c := range list {
		a := artistKey(c.rec)
		rank[a]++
		if len(out) < k && artists[a] && !inCanon[c.rec.MBID] && rank[a] >= 3 && rank[a] <= 8 {
			c.rec.Layer = "deep"
			out = append(out, c)
		}
	}
	return out
}

// isNew says an album came out in the stockNewDays before now; a date without its day or month
// counts from the first of that month or year.
func isNew(date string, now time.Time) bool {
	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, date); err == nil {
			return !t.After(now) && now.Sub(t) <= stockNewDays*24*time.Hour
		}
	}
	return false
}
