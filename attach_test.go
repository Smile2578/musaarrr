package main

import (
	"strings"
	"testing"
)

type attachCase struct {
	title string
	want  string // album title, "" for none
	named bool
}

func checkAlbumFor(t *testing.T, names []string, albums []string, cases []attachCase) {
	t.Helper()
	var als []Album
	byID := map[string]string{"": ""}
	for i, title := range albums {
		id := string(rune('a' + i))
		als = append(als, Album{MBID: id, Title: title})
		byID[id] = title
	}
	var ns [][]string
	for _, n := range names {
		ns = append(ns, words(n))
	}
	keys := albumKeys(als)
	for _, c := range cases {
		id, named := albumFor(c.title, ns, keys)
		if byID[id] != c.want || named != c.named {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", c.title, byID[id], named, c.want, c.named)
		}
	}
}

var bonIver = []string{"Bon Iver, Bon Iver", "For Emma, Forever Ago", "i,i", "22, A Million", "Sable, fable"}

func TestAlbumForBonIver(t *testing.T) {
	checkAlbumFor(t, []string{"Bon Iver"}, bonIver, []attachCase{
		{"Bon Iver - For Emma, Forever Ago (2008) [FLAC]", "For Emma, Forever Ago", true},
		{"Bon Iver - Bon Iver, Bon Iver - 2011, FLAC", "Bon Iver, Bon Iver", true},
		{"(Indie) [WEB] Bon Iver - i,i - 2019", "i,i", true},
		{"Bon_Iver-22_A_Million-WEB-2016-ENRiCH", "22, A Million", true},
		{"Boney M - Rivers of Babylon", "", false},
		{"Taylor.Swift.evermore", "", false},
		{"Bon Iver - Live at AIR Studios", "", true},
	})
}

func TestAlbumForEponymous(t *testing.T) {
	checkAlbumFor(t, []string{"Weezer"}, []string{"Weezer", "Pinkerton"}, []attachCase{
		{"Weezer - Weezer (Blue Album) 1994", "Weezer", true},
		{"Weezer - Hurley", "", true},
		{"Weezer - Pinkerton", "Pinkerton", true},
	})
}

func TestAlbumForTrailingParen(t *testing.T) {
	checkAlbumFor(t, []string{"Bon Iver"}, []string{"Sable, fable (Deluxe)"}, []attachCase{
		{"Bon Iver - Sable, fable - 2025", "Sable, fable (Deluxe)", true},
	})
	// The full title, when one album has it, beats another album's title without its mention.
	checkAlbumFor(t, []string{"Bon Iver"}, []string{"Sable, fable (Deluxe)", "Sable, fable"}, []attachCase{
		{"Bon Iver - Sable, fable - 2025", "Sable, fable", true},
	})
}

func TestAlbumForAlias(t *testing.T) {
	checkAlbumFor(t, []string{"Ye", "Kanye West"}, []string{"Late Registration"}, []attachCase{
		{"Kanye West - 2005 - Late Registration [FLAC]", "Late Registration", true},
		{"Ye - Late Registration", "Late Registration", true},
	})
}

func TestAlbumForLongestWins(t *testing.T) {
	checkAlbumFor(t, []string{"X"}, []string{"Blue", "Blue Lines"}, []attachCase{
		{"X - Blue Lines", "Blue Lines", true},
		{"X - Blue (1971)", "Blue", true},
	})
}

func TestAlbumForRightAfterName(t *testing.T) {
	checkAlbumFor(t, []string{"X"}, []string{"Hits"}, []attachCase{
		{"X - Greatest Hits", "", true},
	})
	checkAlbumFor(t, []string{"Prince"}, []string{"1999"}, []attachCase{
		{"Prince - 1999 (Remastered)", "1999", true},
		{"Prince - 2019 - 1999 [FLAC 24-96]", "1999", true},
	})
}

// A title that goes on after the album's (« Bitches Brew Live », « … Bitches Brew Album ») is another record,
// even when the artist's discography lacks it; a bracket, a comma, a dash or a year ends the album's title.
func TestAlbumForTitleMustEnd(t *testing.T) {
	checkAlbumFor(t, []string{"Miles Davis"}, []string{"Bitches Brew", "Black Beauty: Miles Davis at Fillmore West"}, []attachCase{
		{"Miles.Davis.Bitches.Brew.1969.(Remaster.2013).FLAC.[24bit.96khz]-NOTAG", "Bitches Brew", true},
		{"(Fusion, Avant-Garde, Hard Bop) [CD] Miles Davis - Bitches Brew (2 CD, 2014, MFSL - UDSACD 2-2149) - 1970, FLAC (tracks+.cue), lossless", "Bitches Brew", true},
		{"(Jazz) [LP][24/96] Miles Davis - Bitches Brew, 2LP - 1970, FLAC (tracks)", "Bitches Brew", true},
		{"(Jazz Fusion) [CD] Miles Davis: Bitches Brew (1970, японское издание 2013), FLAC (tracks+.cue), lossless", "Bitches Brew", true},
		{"Miles.Davis.Bitches.Brew.(Ed.2018).1970.SACD.ISO.[DSD64.2.8MHz]-NOTAG", "Bitches Brew", true},
		{"(Jazz) Miles Davis - Bitches Brew (Remaster, 1990) [FLAC (tracks)], lossless", "Bitches Brew", true},
		{"(Jazz-Rock, Fusion) [CD] Miles Davis - Bitches Brew Live - 2011, FLAC (tracks+.cue), lossless", "", true},
		{"Miles.Davis.Bitches.Brew.Live.2011.FLAC", "", true},
		{"(Experimental, Jazz Rock, Fusion) [WEB] The Bitches Brew Band - The Miles Davis Bitches Brew Album - 2026, FLAC (tracks), lossless", "", true},
		{"(Jazz) Miles Davis - Another Bitches Brew - 1971, 1973 FLAC (tracks), lossless", "", true},
		{"Miles Davis - Bitches Brew", "Bitches Brew", true},
		// what follows the title on a tracker: formats, bitrates, rates, edition and pressing words
		{"Miles Davis - Bitches Brew MP3 320", "Bitches Brew", true},
		{"Miles.Davis.Bitches.Brew.Super.Deluxe.Edition.FLAC", "Bitches Brew", true},
		{"Miles.Davis.Bitches.Brew.96kHz.FLAC", "Bitches Brew", true},
		{"Miles Davis - Bitches Brew Limited Edition", "Bitches Brew", true},
		{"Miles Davis Bitches Brew DSD64", "Bitches Brew", true},
		{"Miles Davis - Bitches Brew 40th Anniversary", "Bitches Brew", true},
		{"Miles Davis - Bitches Brew Japanese SHM-CD", "Bitches Brew", true},
		{"Miles Davis - Bitches Brew Legacy", "Bitches Brew", true},
	})
}

func TestWordsAndGaps(t *testing.T) {
	ws, gaps := wordsAndGaps("Hall & Oates - Abandoned Luncheonette (2 CD)")
	if got := strings.Join(ws, "|"); got != "hall|and|oates|abandoned|luncheonette|2|cd" {
		t.Fatalf("words %q", got)
	}
	if got := strings.Join(gaps, "|"); got != "|  |  | - | | (| |)" {
		t.Fatalf("gaps %q", got)
	}
}
