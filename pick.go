package main

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Release is one source found by Lidarr's interactive search (GET /api/v1/release).
type Release struct {
	GUID            string   `json:"guid"`
	IndexerID       int      `json:"indexerId"`
	Title           string   `json:"title"`
	Protocol        string   `json:"protocol"`
	Seeders         *int     `json:"seeders"`
	Approved        bool     `json:"approved"`
	Rejected        bool     `json:"rejected"`
	DownloadAllowed bool     `json:"downloadAllowed"`
	Rejections      []string `json:"rejections"`
	ReleaseWeight   int      `json:"releaseWeight"`
	Quality         struct {
		Quality struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"quality"`
	} `json:"quality"`
}

// nameRejections are Lidarr's refusals that only say it could not read the release name.
// Lidarr matches names on the artist's current MusicBrainz name: "Kanye West - 2005 - Late
// Registration [FLAC]" is a "Wrong album" and "Kanye.West.Yeezus.2013.FLAC" is unreadable
// once the artist is called "Ye".
var nameRejections = []string{"unable to parse release", "wrong album", "unknown artist"}

// refused mirrors a hi-fi quality profile's ignored terms and -10000 formats, which Lidarr never
// applies to a name it could not read.
// shortcut: hard-coded from the first instance's Lidarr (22/11/2026), read them from Lidarr if they change.
var refused = regexp.MustCompile(`(?:^|[^a-z0-9])(?:discograph\w*|integrale|vinyle?|lp[ ._-]?rip|needle[ ._-]?drop|mqa)(?:[^a-z0-9]|$)`)

// pick chooses the source to grab: Lidarr's own first choice, or else the best lossless
// source whose name clearly reads "<artist> [year] <album>" under any of the artist's names.
// forced reports the second case, where Lidarr must be told which album the source is.
func pick(rels []Release, names []string, album string, allowed map[int]bool) (r Release, forced, ok bool) {
	var approved, readable []Release
	titles := [][]string{words(album)}
	if open := strings.LastIndexAny(album, "(["); open > 0 && strings.ContainsAny(album[open:], ")]") {
		titles = append(titles, words(album[:open])) // "Actual Life 3 (January 1 - September 9 2022)" is often just "Actual Life 3"
	}
	for _, rel := range rels {
		switch {
		case rel.Approved:
			approved = append(approved, rel)
		case onlyNameRejections(rel) && allowed[rel.Quality.Quality.ID] && seeded(rel) &&
			!refused.MatchString(fold(rel.Title)) && slices.ContainsFunc(titles, func(t []string) bool { return matchName(words(rel.Title), names, t) }):
			readable = append(readable, rel)
		}
	}
	if len(approved) > 0 { // Lidarr's order, but 24 bits at the highest sample rate first, up to 96 kHz: Lidarr reads no rate
		slices.SortStableFunc(approved, func(a, b Release) int {
			return cmp.Or(cmp.Compare(btoi(!hiRes(a)), btoi(!hiRes(b))), cmp.Compare(rate(b), rate(a)), a.ReleaseWeight-b.ReleaseWeight)
		})
		return approved[0], false, true
	}
	if len(readable) == 0 {
		return Release{}, false, false
	}
	slices.SortStableFunc(readable, func(a, b Release) int {
		if a24, b24 := hiRes(a), hiRes(b); a24 != b24 {
			return boolRank(a24)
		}
		if ra, rb := rate(a), rate(b); ra != rb {
			return cmp.Compare(rb, ra)
		}
		if a.DownloadAllowed != b.DownloadAllowed { // Lidarr follows these to the album itself, no manual import
			return boolRank(a.DownloadAllowed)
		}
		return *b.Seeders - *a.Seeders
	})
	return readable[0], true, true
}

func boolRank(first bool) int {
	if first {
		return -1
	}
	return 1
}

func onlyNameRejections(r Release) bool {
	if len(r.Rejections) == 0 {
		return false
	}
	for _, why := range r.Rejections {
		if !slices.Contains(nameRejections, strings.ToLower(why)) {
			return false
		}
	}
	return true
}

func seeded(r Release) bool {
	return r.Protocol == "usenet" || (r.Seeders != nil && *r.Seeders > 0)
}

// hiRes tells a 24-bit source by Lidarr's quality or by its name: Lidarr reads « 24 Bits » as plain FLAC.
func hiRes(r Release) bool {
	return strings.Contains(r.Quality.Quality.Name, "24") || ReadQuality(r.Title).Bits == 24
}

// rate is the sample rate a source's name gives, 0 when it gives none, capped at 96 kHz:
// a 192 is not heard and weighs twice as much (decided 25/11).
func rate(r Release) float64 { return min(ReadQuality(r.Title).Rate(), 96) }

func fold(s string) string { return accents.Replace(strings.ToLower(s)) }

// words splits a name into folded words: "Björk - 808s & Heartbreak" -> bjork 808s and heartbreak.
func words(s string) []string {
	ws, _ := wordsAndGaps(s)
	return ws
}

// wordsAndGaps is words with what lies before each word, and after the last one: gaps[i] precedes ws[i].
func wordsAndGaps(s string) (ws, gaps []string) {
	s = strings.ReplaceAll(fold(s), "&", " and ")
	start, inWord := 0, false
	for i, r := range s {
		if letter := unicode.IsLetter(r) || unicode.IsDigit(r); letter != inWord {
			if letter {
				gaps = append(gaps, s[start:i])
			} else {
				ws = append(ws, s[start:i])
			}
			start, inWord = i, letter
		}
	}
	if inWord {
		ws, start = append(ws, s[start:]), len(s)
	}
	return ws, append(gaps, s[start:])
}

var tailWords = map[string]bool{"flac": true, "alac": true, "wav": true, "ape": true, "wv": true, "wavpack": true,
	"cd": true, "cdrip": true, "web": true, "webdl": true, "webrip": true, "lossless": true, "hires": true, "hi": true,
	"16bit": true, "24bit": true, "16": true, "24": true, "44": true, "48": true, "96": true, "192": true}

// matchName accepts "<name> <album>" or "<name> <year> <album>", then only edition, year or
// format words: "Kanye.West.Late.Registration.Instrumentals.2005" is not the album.
func matchName(title []string, names []string, album []string) bool {
	if len(album) == 0 {
		return false
	}
	for _, name := range names {
		n := words(name)
		if len(n) == 0 || !startsWith(title, n) {
			continue
		}
		i := len(n)
		if !startsWith(title[i:], album) && i < len(title) && isYear(title[i]) {
			i++
		}
		if !startsWith(title[i:], album) {
			continue
		}
		i += len(album)
		for i < len(title) && edition(title[i]) {
			i++
		}
		if i == len(title) || isYear(title[i]) || tailWords[title[i]] {
			return true
		}
	}
	return false
}

func startsWith(s, prefix []string) bool {
	return len(s) >= len(prefix) && slices.Equal(s[:len(prefix)], prefix)
}

func isYear(w string) bool {
	return len(w) == 4 && (strings.HasPrefix(w, "19") || strings.HasPrefix(w, "20")) && strings.Trim(w, "0123456789") == ""
}

func edition(w string) bool {
	for _, e := range editionWords {
		if strings.HasPrefix(w, e) {
			return true
		}
	}
	return false
}
