package main

import (
	"regexp"
	"strings"
)

// albumKey is an album's title as words: in full, and without a trailing mention ("(Deluxe)").
type albumKey struct {
	MBID        string
	Full, Short []string
}

var trailingMention = regexp.MustCompile(`\s*[(\[][^()\[\]]*[)\]]\s*$`)

func albumKeys(albums []Album) []albumKey {
	keys := make([]albumKey, 0, len(albums))
	for _, al := range albums {
		k := albumKey{MBID: al.MBID, Full: words(al.Title)}
		if s := trailingMention.ReplaceAllString(al.Title, ""); s != al.Title {
			k.Short = words(s)
		}
		if len(k.Full) > 0 {
			keys = append(keys, k)
		}
	}
	return keys
}

// albumFor tells which album a tracker release is: its title holds one of the artist's names,
// then the album's title, with maybe a year or format words between ("Kanye West - 2005 - Late
// Registration"). The longest title wins; a title without its trailing mention is tried only
// when no full title fits. named says the release carries one of the names at all.
func albumFor(title string, names [][]string, keys []albumKey) (mbid string, named bool) {
	t, gaps := wordsAndGaps(title)
	var starts []int // where an album may begin
	for _, n := range names {
		for i := 0; len(n) > 0 && i+len(n) <= len(t); i++ {
			if !startsWith(t[i:], n) {
				continue
			}
			named = true
			for j := i + len(n); ; j++ {
				starts = append(starts, j)
				if j >= len(t) || !(isYear(t[j]) || tailWords[t[j]]) {
					break
				}
			}
		}
	}
	for _, short := range []bool{false, true} {
		best := 0
		for _, k := range keys {
			w := k.Full
			if short {
				w = k.Short
			}
			if len(w) <= best { // the first album wins a tie: albums come before singles
				continue
			}
			for _, j := range starts {
				if startsWith(t[j:], w) && titleEnds(t, gaps, j+len(w)) {
					mbid, best = k.MBID, len(w)
					break
				}
			}
		}
		if mbid != "" {
			return mbid, named
		}
	}
	return "", named
}

// mediumWord is a word saying the release's medium, format, rate or pressing: « 2CD », « LP », « MP3 »,
// « 320 », « 96kHz », « DSD64 », « 40th », « Super », « Japanese »…
var mediumWord = regexp.MustCompile(`^(?:(?:\d+x?)?(?:cds?|lps?|sacd|shmcd|dvda?|vinyle?)|\d+(?:bits?|khz|kbps|k|st|nd|rd|th)?|(?:dsd|dxd)\d*|` +
	`mp3|aac|ogg|opus|m4a|aiff?|mqa|pcm|mono|stereo|japan|japanese|mfsl|super|limited|special|legacy|collectors?|ost|reissue|uk|us|eu|jp)$`)

// titleEnds says the album's title stops before word i: « Bitches Brew (2 CD) », « Bitches Brew - 1970 »,
// « Bitches.Brew.1969 », not « Bitches Brew Live » nor « Bitches.Brew.Live ».
func titleEnds(t, gaps []string, i int) bool {
	return i == len(t) || isYear(t[i]) || tailWords[t[i]] || edition(t[i]) || mediumWord.MatchString(t[i]) ||
		strings.ContainsAny(gaps[i], "()[]{},-–—:;/|+")
}
