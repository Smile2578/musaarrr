package main

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Match says which edition a list of track names (a torrent's files, a Plex album) reproduces.
type Match struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	EditionParts
	Exact   bool     `json:"exact"`             // same tracks, same count
	ByCount bool     `json:"byCount,omitempty"` // names unreadable: only the track count matched
	Extra   []string `json:"extra,omitempty"`   // names no track of the edition explains
	Missing []string `json:"missing,omitempty"` // tracks of the edition no name holds
}

// trackNumber is a file name's track number: « 05 - », « 1-05 », « 101. », « A1 » (a vinyl side).
var trackNumber = regexp.MustCompile(`^\s*[A-Za-z]?\d{1,3}(?:[-.]\d{1,3})?(?:\s*[-._)]+\s*|\s+)`)

// fileTitle is an audio file's name without its track number, so « 05 - 4 Minutes » reads as
// the title « 4 Minutes ». A Plex title carries no number and is never passed through it.
func fileTitle(name string) string {
	if t := trackNumber.ReplaceAllString(name, ""); t != "" {
		return t
	}
	return name
}

// titleWords are what a file name must hold for a track title: the words before a
// parenthesis or a « feat. », since file names often drop the guests.
func titleWords(title string) []string {
	if i := strings.IndexAny(title, "(["); i > 0 {
		title = title[:i]
	}
	ws := words(title)
	for i, w := range ws {
		if i > 0 && (w == "feat" || w == "ft" || w == "featuring") {
			return ws[:i]
		}
	}
	return ws
}

func holds(name, title []string) bool {
	if len(title) == 0 {
		return false
	}
	for _, w := range title {
		if !slices.Contains(name, w) {
			return false
		}
	}
	return true
}

// matchEdition finds the edition names reproduce (track titles, or file names through fileTitle):
// the one whose titles they hold most, at the same track count and disc count when it can. nil
// without names, or when neither a title nor the count points to one. discs 0 means unknown.
func matchEdition(eds []Edition, names []string, discs int) *Match {
	if len(names) == 0 || len(eds) == 0 {
		return nil
	}
	ns := make([][]string, len(names))
	for i, n := range names {
		ns[i] = words(n)
	}
	type scored struct {
		ed      Edition
		used    []bool // names explained by a track
		found   []bool // tracks found in a name
		matched int
	}
	score := func(ed Edition) scored {
		s := scored{ed: ed, used: make([]bool, len(names)), found: make([]bool, len(ed.Tracks))}
		for ti, t := range ed.Tracks {
			tw := titleWords(t.Title)
			for ni := range ns {
				if !s.used[ni] && holds(ns[ni], tw) {
					s.used[ni], s.found[ti] = true, true
					s.matched++
					break
				}
			}
		}
		return s
	}
	var best scored
	better := func(a, b scored) bool { // is a better than b
		if a.matched != b.matched {
			return a.matched > b.matched
		}
		ad, bd := abs(a.ed.Count-len(names)), abs(b.ed.Count-len(names))
		if ad != bd {
			return ad < bd
		}
		return discs > 0 && a.ed.Discs == discs && b.ed.Discs != discs
	}
	for i, ed := range eds {
		if s := score(ed); i == 0 || better(s, best) {
			best = s
		}
	}
	if best.matched == 0 { // names like « Track01 »: only the count can speak
		for _, ed := range eds {
			if ed.Count == len(names) && (discs == 0 || ed.Discs == discs) {
				return &Match{Key: ed.Key, Name: ed.Name, EditionParts: ed.parts(), ByCount: true}
			}
		}
		return nil
	}
	m := &Match{Key: best.ed.Key, Name: best.ed.Name, EditionParts: best.ed.parts()}
	for i, u := range best.used {
		if !u {
			m.Extra = append(m.Extra, names[i])
		}
	}
	for i, f := range best.found {
		if !f {
			m.Missing = append(m.Missing, best.ed.Tracks[i].Title)
		}
	}
	m.Exact = len(m.Extra) == 0 && len(m.Missing) == 0
	return m
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// EditionParts are what an edition's name is made of. The page writes the name from them in the
// reader's language; Name, in English, is for API clients.
type EditionParts struct {
	Country        string `json:"country,omitempty"` // codes or English names, as MusicBrainz gives them
	Year           int    `json:"year,omitempty"`
	Format         string `json:"format,omitempty"`
	Disambiguation string `json:"disambiguation,omitempty"`
	Count          int    `json:"count,omitempty"`
}

func (e Edition) parts() EditionParts {
	return EditionParts{Country: e.Country, Year: e.Year, Format: e.Format, Disambiguation: e.Disambiguation, Count: e.Count}
}

// name is an edition's English name: « JP · 2006 · CD · bonus tracks · 4 tracks ».
func (p EditionParts) name() string {
	var parts []string
	for _, c := range strings.Split(p.Country, ", ") {
		if c != "" {
			parts = append(parts, c)
			break // a release sold in ten countries is named by the first
		}
	}
	if p.Year > 0 {
		parts = append(parts, strconv.Itoa(p.Year))
	}
	if p.Format != "" {
		parts = append(parts, p.Format)
	}
	if p.Disambiguation != "" {
		parts = append(parts, p.Disambiguation)
	}
	n := strconv.Itoa(p.Count) + " track"
	if p.Count != 1 {
		n += "s"
	}
	return strings.Join(append(parts, n), " · ")
}

// nameSays is what a tracker name says about the pressing it was ripped from.
type nameSays struct {
	years   []int  // later than the album: a reissue's
	medium  string // a word of the edition formats (cd, vinyl, digital, sacd); "?" for a medium no edition has
	discs   int    // « 2 CD », « 2LP », « 3 CD »
	country string // a word of the edition countries
	words   []string
}

var (
	discsRe     = regexp.MustCompile(`(?i)\b([1-9])\s?x?\s?(cd|lp)`)
	nameCountry = map[string]string{"japan": "japan", "japanese": "japan", "jpn": "japan", "japon": "japan", "японское": "japan",
		"японский": "japan", "японская": "japan", "us": "states", "usa": "states", "uk": "kingdom", "eu": "europe", "europe": "europe"}
	commonWords = map[string]bool{"edition": true, "release": true, "records": true, "music": true, "the": true, "and": true}
)

func readName(title string, albumYear int) nameSays {
	ws := words(title)
	n := nameSays{words: ws}
	has := func(xs ...string) bool {
		return slices.ContainsFunc(ws, func(w string) bool { return slices.Contains(xs, w) })
	}
	for _, w := range ws {
		if y, _ := strconv.Atoi(w); isYear(w) && y > albumYear && !slices.Contains(n.years, y) {
			n.years = append(n.years, y)
		}
		if c, ok := nameCountry[w]; ok && n.country == "" {
			n.country = c
		}
	}
	switch {
	case has("dvda", "reel", "tape", "cassette"):
		n.medium = "?"
	case has("sacd"):
		n.medium = "sacd"
	case has("lp", "2lp", "3lp", "2xlp", "vinyl", "vinyle", "lprip", "vinylrip"):
		n.medium = "vinyl"
	case has("cd", "2cd", "3cd", "cdrip"):
		n.medium = "cd"
	case has("web", "webrip", "webdl", "digital"):
		n.medium = "digital"
	case ReadQuality(title).Bits == 24: // hi-res and no medium named: a download
		n.medium = "digital"
	}
	if m := discsRe.FindStringSubmatch(title); m != nil {
		n.discs = atoi(m[1])
	}
	if has("mfsl") {
		n.words = append(n.words, "mobile", "fidelity")
	}
	return n
}

// mediumCount is how many discs of medium a format holds: « 2 CD + DVD-Video » holds 2 cd.
func mediumCount(format, medium string) int {
	for _, part := range strings.Split(format, "+") {
		if ws := words(part); slices.Contains(ws, medium) {
			if n, err := strconv.Atoi(ws[0]); err == nil && n < 10 {
				return n
			}
			return 1
		}
	}
	return 0
}

// guessEdition reads a tracker name for the edition it most likely is, among the album's: the one
// with a pressing of the year, medium, disc count, country, label or mention the name gives. nil
// when the name says too little, or names a medium no edition has (a tape, a DVD-Audio).
func guessEdition(eds []Edition, title string, albumYear int) *Match {
	n := readName(title, albumYear)
	if n.medium == "?" { // a tape or a DVD-Audio is none of the editions, whatever its year
		return nil
	}
	in := map[string]int{} // label and mention words -> editions holding them: a word all have says nothing
	for _, ed := range eds {
		seen := map[string]bool{}
		for _, p := range ed.pressed {
			for _, w := range words(p.label + " " + p.disambiguation) {
				if !seen[w] {
					seen[w] = true
					in[w]++
				}
			}
		}
	}
	score := func(p pressing) int {
		s := 0
		switch {
		case slices.Contains(n.years, p.year):
			s += 3
		case len(n.years) == 0 && p.year == albumYear: // no later year named: the first pressing
			s++
		}
		if n.medium != "" {
			if slices.Contains(words(p.format), n.medium) {
				s += 2
			} else {
				s -= 2
			}
		}
		if n.discs > 0 && mediumCount(p.format, n.medium) == n.discs {
			s++
		}
		if n.country != "" && slices.Contains(words(p.country), n.country) {
			s += 2
		}
		for _, w := range words(p.label + " " + p.disambiguation) {
			if len(w) > 2 && !commonWords[w] && in[w] < len(eds) && slices.Contains(n.words, w) {
				s += 2
			}
		}
		return s
	}
	var best *Edition
	var bestP pressing
	top := 1 // a guess needs at least a year and a medium, or a label
	for i := range eds {
		s, sp := math.MinInt, pressing{}
		for _, p := range eds[i].pressed {
			if ps := score(p); ps > s {
				s, sp = ps, p
			}
		}
		if s > top || (best != nil && s == top && eds[i].Pressings > best.Pressings) {
			best, bestP, top = &eds[i], sp, s
		}
	}
	if best == nil {
		return nil
	}
	// named after the pressing the name points to: « Europe · 1990 · 2 CD », not the edition's first vinyl
	p := EditionParts{Country: bestP.country, Year: bestP.year, Format: bestP.format, Disambiguation: bestP.disambiguation, Count: best.Count}
	return &Match{Key: best.Key, Name: p.name(), EditionParts: p}
}

// editionHints reads a release name for what edition it says it is: codes the page words (hint.<code>,
// "pressing:2006" for one with a year), or a name as it is (SHM-CD, Deluxe). year is the album's, 0 if unknown:
// another year in the name is a later pressing.
func editionHints(title string, year int) []string {
	ws := words(title)
	has := func(w ...string) bool {
		return slices.ContainsFunc(ws, func(x string) bool { return slices.Contains(w, x) })
	}
	pair := func(a, b string) bool {
		for i := 0; i+1 < len(ws); i++ {
			if ws[i] == a && ws[i+1] == b {
				return true
			}
		}
		return false
	}
	var out []string
	if has("japan", "japanese", "jpn", "jp", "japon") {
		out = append(out, "japan")
	}
	switch {
	case pair("shm", "cd") || has("shmcd"):
		out = append(out, "SHM-CD")
	case pair("blu", "spec") || has("bluspec"):
		out = append(out, "Blu-spec CD")
	case has("uhqcd"):
		out = append(out, "UHQCD")
	case has("xrcd", "xrcd2", "xrcd24"):
		out = append(out, "XRCD")
	case has("sacd"):
		out = append(out, "SACD")
	}
	if has("mfsl") || pair("mobile", "fidelity") {
		out = append(out, "Mobile Fidelity")
	}
	switch {
	case pair("super", "deluxe"):
		out = append(out, "Super Deluxe")
	case has("deluxe"):
		out = append(out, "Deluxe")
	}
	if has("expanded") {
		out = append(out, "expanded")
	}
	if has("anniversary") {
		out = append(out, "anniversary")
	}
	if has("collector", "collectors") {
		out = append(out, "collector")
	}
	if has("box", "boxset", "coffret") {
		out = append(out, "box")
	}
	if has("mono") {
		out = append(out, "mono")
	}
	used := ""
	for i, w := range ws {
		if strings.HasPrefix(w, "remaster") {
			r := "remaster"
			for _, j := range []int{i + 1, i - 1} { // « 1969 (Remaster 2013) »: the year of the sessions is not the remaster's
				if j >= 0 && j < len(ws) && isYear(ws[j]) && (year == 0 || atoi(ws[j]) > year) {
					r, used = "remaster:"+ws[j], ws[j]
					break
				}
			}
			out = append(out, r)
			break
		}
	}
	if year > 0 {
		for _, w := range ws {
			if isYear(w) && w != used && atoi(w) > year { // a year before the album's is a recording's, not a pressing's
				out = append(out, "pressing:"+w)
				break
			}
		}
	}
	if has("bonus") {
		out = append(out, "bonus")
	}
	return out
}
