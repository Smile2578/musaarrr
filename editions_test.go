package main

import (
	"slices"
	"testing"
)

func lateRegEditions(t *testing.T) []Edition {
	al, err := newAlbumMetadata(t, lateRegJSON).Album(ctx, lateRegMBID)
	if err != nil {
		t.Fatal(err)
	}
	return al.Editions
}

func TestMatchEditionByTitles(t *testing.T) {
	eds := lateRegEditions(t)
	m := matchEdition(eds, []string{"01 - Wake Up Mr. West", "02 - Heard 'Em Say (feat. Adam Levine)", "03 Touch The Sky", "04. Late"}, 1)
	if m == nil || m.Key != "r-jp" || !m.Exact || len(m.Extra) != 0 || len(m.Missing) != 0 {
		t.Fatalf("got %+v", m)
	}
	m = matchEdition(eds, []string{"01 Wake Up Mr. West", "02 Heard Em Say", "03 Touch the Sky"}, 1)
	if m == nil || m.Key != "r-gb" || !m.Exact {
		t.Fatalf("got %+v", m)
	}
}

func TestMatchEditionExtraAndMissing(t *testing.T) {
	eds := lateRegEditions(t)
	m := matchEdition(eds, []string{"01 Wake Up Mr. West", "02 Heard Em Say", "03 Touch the Sky", "04 Late", "05 Diamonds (Remix)", "06 Gold Digger (Live)"}, 1)
	if m == nil || m.Key != "r-jp" || m.Exact || !slices.Equal(m.Extra, []string{"05 Diamonds (Remix)", "06 Gold Digger (Live)"}) {
		t.Fatalf("got %+v", m)
	}
	m = matchEdition(eds, []string{"01 Wake Up Mr. West", "03 Touch the Sky"}, 1)
	if m == nil || m.Key != "r-gb" || m.Exact || !slices.Equal(m.Missing, []string{"Heard 'em Say"}) {
		t.Fatalf("got %+v", m) // the closest edition, and what the source lacks
	}
}

func TestMatchEditionByCount(t *testing.T) {
	eds := lateRegEditions(t)
	m := matchEdition(eds, []string{"Track01", "Track02", "Track03", "Track04"}, 1)
	if m == nil || m.Key != "r-jp" || !m.ByCount || m.Exact {
		t.Fatalf("got %+v", m) // file names say nothing: only the count speaks
	}
	if matchEdition(eds, nil, 0) != nil {
		t.Fatal("no file, no edition")
	}
}

func TestMatchEditionDiscs(t *testing.T) {
	two := Edition{Key: "two", Discs: 2, Count: 4, Tracks: []Track{{1, 1, "A", 0}, {1, 2, "B", 0}, {2, 1, "C", 0}, {2, 2, "D", 0}}}
	one := Edition{Key: "one", Discs: 1, Count: 4, Tracks: []Track{{1, 1, "W", 0}, {1, 2, "X", 0}, {1, 3, "Y", 0}, {1, 4, "Z", 0}}}
	m := matchEdition([]Edition{one, two}, []string{"01", "02", "01", "02"}, 2)
	if m == nil || m.Key != "two" {
		t.Fatalf("got %+v", m) // same count, the disc count decides
	}
}

func TestEditionName(t *testing.T) {
	eds := lateRegEditions(t)
	if got := eds[1].Name; got != "JP · 2006 · SHM-CD · bonus tracks · 4 tracks" {
		t.Errorf("got %q", got)
	}
	if got := (Edition{Country: "XE", Format: "2 CD", Year: 2017, Discs: 2, Count: 23}).parts().name(); got != "XE · 2017 · 2 CD · 23 tracks" {
		t.Errorf("got %q", got)
	}
	if got := (Edition{Format: "Digital Media", Count: 1}).parts().name(); got != "Digital Media · 1 track" {
		t.Errorf("got %q", got)
	}
	if got := (Edition{Country: "United States", Format: "2 CD", Year: 1999, Count: 7}).parts().name(); got != "United States · 1999 · 2 CD · 7 tracks" {
		t.Errorf("a country by its name: got %q", got)
	}
}

func TestEditionHints(t *testing.T) {
	for _, c := range []struct {
		title string
		year  int
		want  []string
	}{
		{"Kanye West - Late Registration (2012 Japan SHM-CD) FLAC", 2005, []string{"japan", "SHM-CD", "pressing:2012"}},
		{"Radiohead - OK Computer (Deluxe Edition) [2009 Remaster] FLAC", 1997, []string{"Deluxe", "remaster:2009"}},
		{"Radiohead.OK.Computer.1997.FLAC", 1997, nil},
		{"Michael Jackson - Thriller (25th Anniversary Edition) [Remastered]", 1982, []string{"anniversary", "remaster"}},
		{"Aznavour - Intégrale (Coffret 40 CD) Box Set Mono", 0, []string{"box", "mono"}},
	} {
		if got := editionHints(c.title, c.year); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.title, got, c.want)
		}
	}
}

func TestMatchEditionNumericTitles(t *testing.T) {
	ed := Edition{Key: "1999", Discs: 1, Count: 3, Tracks: []Track{{1, 1, "1999", 0}, {1, 2, "Little Red Corvette", 0}, {1, 3, "4 Minutes", 0}}}
	m := matchEdition([]Edition{ed}, []string{"1999", "Little Red Corvette", "4 Minutes"}, 1) // Plex titles carry no number
	if m == nil || !m.Exact {
		t.Fatalf("got %+v", m)
	}
}

func TestMatchEditionNothingSpeaks(t *testing.T) {
	eds := lateRegEditions(t) // 3 and 4 tracks
	if m := matchEdition(eds, []string{"Track01", "Track02", "Track03", "Track04", "Track05", "Track06", "Track07", "Track08", "Track09"}, 1); m != nil {
		t.Fatalf("no title and no count match, yet %+v", m)
	}
}

func TestFileTitle(t *testing.T) {
	for in, want := range map[string]string{
		"05 - 4 Minutes": "4 Minutes", "1999": "1999", "03. 1999": "1999", "1-05 Delirious": "Delirious", "101. Intro": "Intro",
		"A1 Little Red Corvette": "Little Red Corvette", "Track01": "Track01", "05_Free": "Free", "07 22 (OVER S∞∞N)": "22 (OVER S∞∞N)",
		"A Day in the Life": "A Day in the Life", "01": "01",
	} {
		if got := fileTitle(in); got != want {
			t.Errorf("fileTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// bitchesBrew is Bitches Brew's editions as the metadata server groups them: the original tracklist
// pressed on vinyl and CD, the 1999 reissue with « Feio » (pressed again in Japan in 2013), a 2013
// download, the 2010 Legacy Edition, the 40th anniversary box and Mobile Fidelity's SACD.
func bitchesBrew() []Edition {
	return []Edition{
		{Key: "orig", Name: "États-Unis · 1970 · 2 Vinyle 12\" · 6 pistes", Count: 6, Discs: 2, Pressings: 3, pressed: []pressing{
			{year: 1970, country: "United States", format: "2 12\" Vinyl", label: "Columbia"},
			{country: "Europe", format: "2 CD", label: "CBS", disambiguation: "CBS Jazz Masterpieces"},
			{year: 1990, country: "Europe", format: "2 CD", label: "CBS"}}},
		{Key: "feio", Name: "États-Unis · 1999 · 2 CD · 7 pistes", Count: 7, Discs: 2, Pressings: 2, pressed: []pressing{
			{year: 1999, country: "United States", format: "2 CD", label: "Columbia, Legacy"},
			{year: 2013, country: "Japan", format: "2 CD", label: "Sony Music Japan"}}},
		{Key: "web13", Name: "Monde · 2013 · Numérique · 6 pistes", Count: 6, Discs: 1, Pressings: 1, pressed: []pressing{
			{year: 2013, country: "[Worldwide]", format: "Digital Media", label: "Columbia"}}},
		{Key: "legacy", Name: "Europe · 2010 · 2 CD + DVD-Video · 12 pistes", Count: 12, Discs: 2, Pressings: 2, pressed: []pressing{
			{year: 2010, country: "Europe", format: "2 CD + DVD-Video", label: "Columbia, Legacy", disambiguation: "European release"},
			{year: 2010, country: "United States", format: "2 CD + DVD-Video", label: "Columbia, Legacy", disambiguation: "40th Anniversary Legacy Edition"}}},
		{Key: "box", Name: "Europe · 2010 · 2 Vinyle 12\" + 3 CD + DVD-Video · 34 pistes", Count: 34, Discs: 6, Pressings: 1, pressed: []pressing{
			{year: 2010, country: "Europe", format: "2 12\" Vinyl + 3 CD + DVD-Video", label: "Columbia, Legacy", disambiguation: "40th Anniversary Collector's Edition"}}},
		{Key: "mfsl", Name: "États-Unis · 2014 · 2 SACD hybride · 12 pistes", Count: 12, Discs: 4, Pressings: 1, pressed: []pressing{
			{year: 2014, country: "United States", format: "2 Hybrid SACD (CD layer) + 2 Hybrid SACD (SACD layer)", label: "Columbia, Mobile Fidelity Sound Lab"}}},
	}
}

func TestGuessEditionFromName(t *testing.T) {
	eds := bitchesBrew()
	for _, c := range []struct{ title, want string }{
		{"(Fusion, Avant-Garde, Hard Bop) [CD] Miles Davis - Bitches Brew (2 CD, 2014, MFSL - UDSACD 2-2149) - 1970, FLAC (tracks+.cue), lossless", "mfsl"},
		{"(Jazz Fusion, Avant-Garde Jazz, Jazz-Rock) [CD] Miles Davis - Bitches Brew - 1970 (1999), FLAC (tracks+.cue), lossless", "feio"},
		{"(Jazz Fusion) [CD] Miles Davis: Bitches Brew (1970, японское издание 2013), FLAC (tracks+.cue), lossless", "feio"},
		{"Miles.Davis.Bitches.Brew.1969.(Remaster.2013).FLAC.[24bit.96khz]-NOTAG", "web13"}, // hi-res and no medium: a download
		{"(Fusion, Jazz-Rock) [CD] Miles Davis - Bitches Brew (1970) (3 CD) - 2010, FLAC (tracks+.cue), lossless", "box"},
		{"(Jazz/Fusion) Miles Davis - Bitches Brew (1969) (40th Anniversary Legacy Edition) - 2010, FLAC (image+.cue) lossless", "legacy"},
		{"(Jazz Fusion / Experimental Jazz) [LP] [24/96] Miles Davis - Bitches Brew - 1970 (US), FLAC (tracks), lossless", "orig"},
		{"(Avant-Garde/Fusion) Miles Davis - Bitches Brew [2 CD] - 1969, APE (image+.cue), lossless", "orig"},
		{"(Avant-Garde Jazz / Fusion) Miles Davis 1970-Bitches Brew [1990, Remastered Issue, CBS 460602 2], FLAC (image+.cue), lossless", "orig"},
		{"[DVDA][QU] Miles Davis - Bitches Brew - 1969 (Avant-Garde, Fusion)", ""},                                             // a quad DVD-Audio: no such edition
		{"[DVDA] Miles Davis - Bitches Brew - 1970 (1999 USA)", ""},                                                            // even with a year and a country that fit
		{"(Fusion, Jazz Rock) [MB] [24/96] Miles Davis - Bitches Brew - 1970 (Reel-To-Reel, 3 ¾ ips), FLAC (tracks+.cue)", ""}, // a tape
	} {
		got := ""
		if m := guessEdition(eds, c.title, 1970); m != nil {
			got = m.Key
			if m.Name == "" || m.Exact {
				t.Errorf("%s: %+v", c.title, m)
			}
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.title, got, c.want)
		}
	}
	if guessEdition(nil, "Miles Davis - Bitches Brew [2 CD]", 1970) != nil {
		t.Error("an edition without editions")
	}
	// named after the pressing the name points to, not the edition's first: a 2 CD is no vinyl
	if m := guessEdition(eds, "Miles Davis 1970-Bitches Brew [1990, Remastered Issue, CBS 460602 2]", 1970); m == nil || m.Name != "Europe · 1990 · 2 CD · 6 tracks" {
		t.Errorf("1990 CBS: %+v", m)
	}
}

func TestEditionHintsYears(t *testing.T) {
	// the remaster's year is the one after the word when it is not the album's; a year before the album is a session's
	got := editionHints("Miles.Davis.Bitches.Brew.1969.(Remaster.2013).FLAC.[24bit.96khz]-NOTAG", 1970)
	if !slices.Equal(got, []string{"remaster:2013"}) {
		t.Errorf("got %q", got)
	}
}
