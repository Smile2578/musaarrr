package main

import (
	"slices"
	"testing"
)

func TestReadQuality(t *testing.T) {
	withTitleTags(t, "[HR24]", "[MC]")
	cases := []struct {
		title  string
		format string
		bits   int
		khz    string
		layout string
		flags  []string
		level  int
	}{
		{"(Indie Folk | Rock) [WEB] Bon Iver - Sable, fable - 2025, FLAC (tracks), lossless", "FLAC", 0, "", "tracks", nil, 1},
		{"[HR24][OF][LDR] Bon Iver - Sable, fable - 2025 (Folk-Rock, Indie Folk)", "FLAC", 24, "", "", nil, 0},
		{"(Indie Folk) [CD] Bon Iver - i,i - 2019, FLAC (tracks+.cue), lossless", "FLAC", 0, "", "tracks", nil, 1},
		{"X - Y - 2020, FLAC (image+.cue), lossless", "FLAC", 0, "", "image", nil, 1}, // musictag cuts it into tracks
		{"X - Y - 2020, FLAC (image + .cue), lossless", "FLAC", 0, "", "image", nil, 1},
		{"(Folk Rock / Indie Rock) [LP] [24/96] Bon Iver - Bon Iver, Bon Iver - 2011 (US), FLAC (tracks), lossless", "FLAC", 24, "96", "tracks", []string{"vinyl"}, 2},
		{"(Indie folk, chamber Pop, folktronica) [CD`4|WEB`2] Bon Iver - Дискография - 2007-2019, ALAC (tracks+.cue), lossless", "ALAC", 0, "", "tracks", []string{"discography"}, 1},
		{"(Folk-rock, Indie Folk) Bon Iver - Дискография: 26 релизов (2008-2026), MP3, 128-320 kbps", "MP3", 0, "", "", []string{"discography"}, 6},
		{"(Alternative, Singer/Songwriter, Electronic, Indie Rock) Bon Iver - 22, A Million - 2016, AAC (tracks), 256 kbps (WEB)", "AAC", 0, "", "tracks", nil, 6},
		{"Taylor.Swift.evermore.Explicit.24BIT.88.2KHZ.WEB.FLAC.2020", "FLAC", 24, "88.2", "", nil, 0},
		{"[JAZZ] - Matt Carmichael - Dancing with Embers - 2025 - FLAC 16BITS 44 1KHZ-EICHBAUM", "FLAC", 16, "44.1", "", nil, 1},
		{"Taylor Swift - Folklore (2020) [Flac 16/44]", "FLAC", 16, "44.1", "", nil, 1},
		{"Artiste.Album.2005.FLAC.[16bit.44.1kHz]-GRP", "FLAC", 16, "44.1", "", nil, 1},
		{"Artiste - Album [FLAC 24bit Lossless / WEB]", "FLAC", 24, "", "", nil, 0},
		{"Artiste - Album (2003) [ISO SACD]", "ISO", 0, "", "", nil, 4},
		{"Artiste - Album [DSD64]", "DSD", 0, "", "", nil, 4},
		{"Artiste - Album (2019) [FLAC 24-96 MQA]", "FLAC", 24, "96", "", []string{"MQA"}, 2},
		{"[MC] Bon Iver - Bon Iver, Bon Iver (5.1 mix) - 2011/2012 (Post-Rock)", "", 0, "", "", []string{"multichannel"}, 5},
		{"[LP] Bon Iver - 22, A Million (2016) - 2016 (Folk,ndie, alternative)", "", 0, "", "", []string{"vinyl"}, 5},
		{"Bruno Mars - 24K Magic (2016) [FLAC 16/44]", "FLAC", 16, "44.1", "", nil, 1},
		{"Eminem - The Marshall Mathers LP (2000) FLAC", "FLAC", 0, "", "", nil, 1},
		{"Bon Iver - Bon Iver, Bon Iver (2011) [MP3 CBR 320KBPS]", "MP3", 0, "", "", nil, 6},
		{"(Indie/Folk) Bon Iver - Коллекция: 2008-2011 (3 CD+covers), FLAC (tracks+.cue), lossless", "FLAC", 0, "", "tracks", []string{"discography"}, 1},
		{"Bon.Iver.For.Emma.Forever.Ago.2008.WEB.mp3.320kbps-NoTag", "MP3", 0, "", "", nil, 6},
		{"Logic - Vinyl Days (2022) [FLAC 24bit]", "FLAC", 24, "", "", nil, 0},
		{"Artiste - Album (1975) [FLAC 24/96 Vinyl Rip]", "FLAC", 24, "96", "", []string{"vinyl"}, 2},
		{"Artiste - Album (1975) (Vinyl) FLAC", "FLAC", 0, "", "", []string{"vinyl"}, 2},
		{"(Jazz) [LP][24/96] Miles Davis - Bitches Brew, 2LP - 1970, FLAC (tracks)", "FLAC", 24, "96", "tracks", []string{"vinyl"}, 2},
		{"(Fusion) [24/96] Miles Davis - Bitches Brew (2 LP) - 1969/2008, FLAC (tracks)", "FLAC", 24, "96", "tracks", []string{"vinyl"}, 2},
		{"(Jazz) Miles Davis - Bitches Brew (1969) 2 LP's - 2008, FLAC (image+.cue), 24bit/96kHz", "FLAC", 24, "96", "image", []string{"vinyl"}, 2},
		{"(Jazz, Fusion) [2xLP] [32/176,4] Miles Davis - Bitches Brew - 1970, WavPack (image+.cue) UK 1st", "WavPack", 0, "", "image", []string{"vinyl"}, 2},
		{"Miles Davis - Kind of Blue (LP, 1959) FLAC", "FLAC", 0, "", "", []string{"vinyl"}, 2},
		{"Artist - Album [lp] FLAC", "FLAC", 0, "", "", []string{"vinyl"}, 2},
		{"Artist - Album 2lp FLAC", "FLAC", 0, "", "", []string{"vinyl"}, 2},
		{"(Jazz) [24/96] Miles Davis - Bitches Brew - 1970 (Reel-To-Reel, 3 ¾ ips), FLAC (tracks+.cue)", "FLAC", 24, "96", "tracks", nil, 0},
		{"X - Y (2010) [24/96] FLAC (image+.cue)", "FLAC", 24, "96", "image", nil, 0},
	}
	lossless := map[string]bool{"FLAC": true, "ALAC": true, "WavPack": true, "APE": true, "DSD": true, "ISO": true}
	for _, c := range cases {
		q := ReadQuality(c.title)
		flags := c.flags
		if flags == nil {
			flags = []string{}
		}
		if q.Format != c.format || q.Bits != c.bits || q.KHz != c.khz || q.Layout != c.layout || !slices.Equal(q.Flags, flags) || q.Lossless != lossless[c.format] || q.Level() != c.level {
			t.Errorf("%s\n got %+v level %d\nwant %s %d %q %q %v level %d", c.title, q, q.Level(), c.format, c.bits, c.khz, c.layout, flags, c.level)
		}
		if q.Flags == nil {
			t.Errorf("%s: nil flags would reach the browser as null", c.title)
		}
	}
}

func TestQualityRate(t *testing.T) {
	for khz, want := range map[string]float64{"96": 96, "88.2": 88.2, "44.1": 44.1, "": 0} {
		if got := (Quality{KHz: khz}).Rate(); got != want {
			t.Errorf("%q: got %v, want %v", khz, got, want)
		}
	}
}

// withTitleTags sets the release-title tags for one test, as the settings would.
func withTitleTags(t *testing.T, hiRes, multichannel string) {
	t.Helper()
	setTitleTags(hiRes, multichannel)
	t.Cleanup(func() { setTitleTags("", "") })
}

// No indexer's tags are built in: they come from the settings, and name nothing by default.
func TestTitleTagsComeFromTheSettings(t *testing.T) {
	const hr, mc = "[HR24][OF] A - B - 2020", "[MC] A - B (mix) - 2011"
	if q := ReadQuality(hr); q.Bits != 0 || slices.Contains(ReadQuality(mc).Flags, "multichannel") {
		t.Fatalf("a tag read without being set: %+v", q)
	}
	withTitleTags(t, " [hr24] , [X24]", "[MC]")
	if q := ReadQuality(hr); q.Bits != 24 || q.Format != "FLAC" || !q.Lossless {
		t.Errorf("hi-res tag: %+v", q)
	}
	if q := ReadQuality(mc); !slices.Contains(q.Flags, "multichannel") {
		t.Errorf("multichannel tag: %+v", q)
	}
}
