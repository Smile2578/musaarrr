package main

import "testing"

// rel builds a release as Lidarr's interactive search returns it.
func rel(title string, qualityID, seeders int, rejections ...string) Release {
	r := Release{GUID: title, IndexerID: 1, Title: title, Protocol: "torrent", Seeders: &seeders,
		Rejected: len(rejections) > 0, Approved: len(rejections) == 0, Rejections: rejections, DownloadAllowed: true}
	for _, why := range rejections {
		r.DownloadAllowed = r.DownloadAllowed && why != "Unable to parse release" // as Lidarr 3.1.6 answers
	}
	r.Quality.Quality.ID, r.Quality.Quality.Name = qualityID, qualityNames[qualityID]
	return r
}

const (
	qUnknown = 0
	qMP3     = 4
	qFLAC    = 6
	qFLAC24  = 21
)

var qualityNames = map[int]string{qUnknown: "Unknown", qMP3: "MP3-320", qFLAC: "FLAC", qFLAC24: "FLAC 24bit"}

var hifi = map[int]bool{qFLAC: true, qFLAC24: true}
var ye = []string{"Ye", "Kanye West", "Kanye"}

func TestPickPrefersLidarrApproved(t *testing.T) {
	rels := []Release{
		rel("Kanye.West.Graduation.2007.FLAC-GRP", qFLAC, 50, "Unable to parse release"),
		rel("Kanye West - Graduation (2007) [FLAC 16bits 44.1kHz]", qFLAC, 3),
	}
	got, forced, ok := pick(rels, ye, "Graduation", hifi)
	if !ok || forced || got.Title != rels[1].Title {
		t.Fatalf("got %q forced=%v ok=%v", got.Title, forced, ok)
	}
}

func TestPickApprovedByLidarrWeight(t *testing.T) {
	a, b := rel("A", qFLAC, 1), rel("B", qFLAC24, 1)
	a.ReleaseWeight, b.ReleaseWeight = 2, 1
	if got, _, _ := pick([]Release{a, b}, ye, "x", hifi); got.Title != "B" {
		t.Fatalf("got %q, want Lidarr's first choice", got.Title)
	}
}

// Late Registration as trackers list it: Lidarr rejects both FLAC sources.
func TestPickUnreadableName(t *testing.T) {
	rels := []Release{
		rel("Kanye.West.Late.Registration.2005.CD.FLAC.16Bit.44.1Khz-BEN1", qFLAC, 2, "Unable to parse release"),
		rel("Kanye West - 2005 - Late Registration [FLAC - 44.1 kHz / 16 bits]", qFLAC, 7, "Wrong album"),
	}
	got, forced, ok := pick(rels, ye, "Late Registration", hifi)
	if !ok || !forced || got.Seeders == nil || *got.Seeders != 7 {
		t.Fatalf("got %q forced=%v ok=%v", got.Title, forced, ok)
	}
}

func TestPickForcedPrefers24Bit(t *testing.T) {
	rels := []Release{
		rel("Ye.Donda.2021.FLAC.16bit-A", qFLAC, 40, "Unable to parse release"),
		rel("Kanye.West.Donda.2021.WEB.FLAC.24bit-B", qFLAC24, 2, "Unable to parse release"),
	}
	if got, _, _ := pick(rels, ye, "Donda", hifi); got.Title != rels[1].Title {
		t.Fatalf("got %q", got.Title)
	}
}

func TestPickMatchesName(t *testing.T) {
	cases := []struct {
		title, album string
		want         bool
	}{
		{"Kanye.West.Yeezus.2013.WEB.FLAC.Lossless-EXTRAP", "Yeezus", true},
		{"Kanye.West.808s.and.Heartbreak.2008.FLAC-GRP", "808s & Heartbreak", true},
		{"Kanye West - The Life of Pablo (Deluxe Edition) 2016 FLAC", "The Life of Pablo", true},
		{"KANYE WEST - MY BEAUTIFUL DARK TWISTED FANTASY", "My Beautiful Dark Twisted Fantasy", true},
		{"Kanye.West.ye.2018.FLAC-GRP", "ye", true},
		{"Kanye.West.Ye.Vs.The.People.2018.FLAC", "ye", false}, // another song that starts like the album
		{"Kanye.West.Late.Registration.Instrumentals.2005.FLAC", "Late Registration", false},
		{"Pink.Floyd.The.Dark.Side.Of.The.Moon.1973.FLAC", "Late Registration", false},
		{"Kanye.Westside.Late.Registration.2005.FLAC", "Late Registration", false},
		{"Late.Registration.Kanye.West.2005.FLAC", "Late Registration", false},
	}
	for _, c := range cases {
		r := rel(c.title, qFLAC, 5, "Unable to parse release")
		if _, _, ok := pick([]Release{r}, ye, c.album, hifi); ok != c.want {
			t.Errorf("%q for %q: got %v", c.title, c.album, ok)
		}
	}
	fred := []string{"Fred again.."}
	for _, c := range []struct {
		title, album string
		want         bool
	}{
		{"Fred again.. - Actual Life 3 (2022) [FLAC]", "Actual Life 3 (January 1 - September 9 2022)", true},
		{"Fred.again.Actual.Life.3.January.1.September.9.2022.2022.FLAC-GRP", "Actual Life 3 (January 1 - September 9 2022)", true},
		{"Fred again.. - Actual Life 2 (2021) [FLAC]", "Actual Life (April 14 - December 17 2020)", false},
		{"Fred again.. - Actual Life (2021) [FLAC]", "Actual Life 2 (February 2 - October 15 2021)", false},
	} {
		r := rel(c.title, qFLAC, 5, "Unable to parse release")
		if _, _, ok := pick([]Release{r}, fred, c.album, hifi); ok != c.want {
			t.Errorf("%q for %q: got %v", c.title, c.album, ok)
		}
	}
	bjork := rel("Bjork.Homogenic.1997.FLAC", qFLAC, 5, "Unable to parse release")
	if _, _, ok := pick([]Release{bjork}, []string{"Björk"}, "Homogenic", hifi); !ok {
		t.Error("accents not folded")
	}
}

func TestPickRefusesWhatLidarrWouldRefuse(t *testing.T) {
	cases := []Release{
		rel("Kanye West - 2008 - 808s & Heartbreak [44.1 kHz / 16 bits]", qUnknown, 9, "Wrong album"),     // quality unknown
		rel("Kanye.West.Late.Registration.2005.MP3.320-GRP", qMP3, 9, "Unable to parse release"),          // lossy
		rel("Kanye.West.Late.Registration.2005.FLAC-GRP", qFLAC, 0, "Unable to parse release"),            // no seeder
		rel("Kanye West - Late Registration (2005) [Vinyl FLAC 24bit]", qFLAC24, 9, "Wrong album"),        // vinyl rip
		rel("Kanye.West.Late.Registration.2005.MQA.FLAC-GRP", qFLAC24, 9, "Unable to parse release"),      // MQA
		rel("Kanye West - Late Registration 2005 Discography FLAC", qFLAC, 9, "Wrong album"),              // pack
		rel("Kanye.West.Late.Registration.2005.FLAC-GRP", qFLAC, 9, "Unable to parse release", "Too big"), // another reason
	}
	for _, r := range cases {
		if got, _, ok := pick([]Release{r}, ye, "Late Registration", hifi); ok {
			t.Errorf("took %q", got.Title)
		}
	}
}

// What Lidarr's interactive search returned on a real instance on 22/11/2026, and the pick expected.
func TestPickRealSearches(t *testing.T) {
	fred := []string{"Fred again.."}
	cases := []struct {
		album string
		names []string
		rels  []Release
		want  string // "" when nothing should be grabbed
	}{
		{"Actual Life (April 14 - December 17 2020)", fred, []Release{
			rel("Fred.again.Actual.Life.(April.14.-.December.17.2020).2021.FLAC.[24bit.44.1kHz]-egging5688", qFLAC24, 8, "Unknown Artist"),
		}, "Fred.again.Actual.Life.(April.14.-.December.17.2020).2021.FLAC.[24bit.44.1kHz]-egging5688"},
		{"Donda", ye, []Release{
			rel("Kanye West - 2021 -  Donda [FLAC - 44.1 kHz / 16 bits]", qFLAC, 7, "Wrong album"),
			rel("Groovy.Aardvark.Exit.Stage.Dive.Live.Remastered.1999.FLAC.[16bit.44.1kHz]-NOTAG", qFLAC, 6, "Unable to parse release"),
			rel("George.Russell.Essential.Jazz.Masters.2010.FLAC.[16bit.44.1kHz]-PK", qFLAC, 6, "Unable to parse release"),
		}, "Kanye West - 2021 -  Donda [FLAC - 44.1 kHz / 16 bits]"},
		{"ye", ye, []Release{ // the riskiest of the 198 sources seen for the album "ye": none is it
			rel("Ye.Ren.Another.Shining.Path.1999.FLAC.[16bit.44.1kHz]-SAUROPODE", qFLAC, 3, "Unable to parse release"),
			rel("Sizzla.Praise.Ye.Jah.1997.FLAC.[16Bit.44.1kHZ]-NOTAG", qFLAC, 7, "Unable to parse release"),
			rel("Quantic.Nidia.Gongora.E.Ye.Ye.(EP).2017.FLAC.[16bit.44.1kHz]-PK", qFLAC, 30, "Unable to parse release"),
			rel("YE.BULLY.2026.FLAC.[24bit.44.1kHz]-CTBRL", qFLAC24, 15, "Unable to parse release"),
			rel("Kanye.West.Ye.Bully.Deluxe.24BIT.44.1KHZ.WEB.FLAC.2026-KursK", qFLAC24, 3, "Unable to parse release"),
			rel("Kany.West.Ye.Bully.Deluxe.16BIT.44.1KHZ.WEB.FLAC.2026-KursK", qFLAC, 4, "Unable to parse release"),
			rel("La.Yegros.Haz.16BIT.44.1KHZ.CD.FLAC.2024-NoTag", qFLAC, 7, "Unable to parse release"),
		}, ""},
		{"Jesus Is King", ye, []Release{
			rel("VA.Rejoice.the.Lord.is.King.Great.Hymns.from.Westminster.Abbey.Robert.Quinney.The.Choir.of.Westminster.Abbey.James.O.donnell.2014.FLAC.[24bit.88.2kHz]-NOTAG", qFLAC24, 7, "Unable to parse release"),
			rel("Phil.Glass.Tradicional.Hymns.(Phil.Glass).2013.FLAC.[16bit.44.1kHz]-PK", qFLAC, 6, "Unable to parse release"),
			rel("Johann.Sebastian.Bach.Classical.Music.for.the.Reader.Christmas.Edition.2015.FLAC.[16bit.44.1kHz]-PK", qFLAC, 12, "Unable to parse release"),
			rel("Donna.Summer.Lady.Of.The.Night.2020.Web.FLAC.16BIT.44.1KHZ-NoTag", qFLAC, 7, "Unable to parse release"),
			rel("Elvis Presley - The Complete Elvis Presley Masters - [Flac 16 Bits 44.1 kHz]", qFLAC, 10, "Unknown Artist"),
		}, ""},
		{"My Beautiful Dark Twisted Fantasy", ye, []Release{
			rel("Kanye West - 2010 - My Beautiful Dark Twisted Fantasy [FLAC - 44.1 kHz / 16 bits]", qFLAC, 12, "Wrong album"),
			rel("Kanye.West.My.Beautiful.Dark.Twisted.Fantasy.2010.WEB.FLAC.Lossless-EXTRAP", qFLAC, 1, "Unable to parse release"),
		}, "Kanye West - 2010 - My Beautiful Dark Twisted Fantasy [FLAC - 44.1 kHz / 16 bits]"},
		{"808s & Heartbreak", ye, []Release{
			rel("Kanye West - 2008 - 808s & Heartbreak [44.1 kHz / 16 bits]", qUnknown, 5,
				"278.3 MB is larger than maximum allowed 133.6 MB", "Unknown is not wanted in profile", "Wrong album"),
			rel("Kanye.West.808s.And.Heartbreak.2008.WEB.FLAC.Lossless-EXTRAP", qFLAC, 2, "Unable to parse release"),
		}, "Kanye.West.808s.And.Heartbreak.2008.WEB.FLAC.Lossless-EXTRAP"},
		{"The Life of Pablo", ye, []Release{
			rel("Kanye West - 2016 - The Life of Pablo [FLAC - 44.1 kHz / 16 bits]", qFLAC, 7, "Wrong album"),
			rel("Various.Artists.Trojan.Reggae.Box.Set.Collection.44.1KHZ.CD.2007-atrona", qUnknown, 5, "Unable to parse release"),
			rel("Kanye.West.The.Life.Of.Pablo.2016.WEB.FLAC.Lossless-EXTRAP", qFLAC, 1, "Unable to parse release"),
		}, "Kanye West - 2016 - The Life of Pablo [FLAC - 44.1 kHz / 16 bits]"},
	}
	for _, c := range cases {
		got, _, ok := pick(c.rels, c.names, c.album, hifi)
		if c.want == "" && ok || c.want != "" && got.Title != c.want {
			t.Errorf("%s: got %q (ok=%v), want %q", c.album, got.Title, ok, c.want)
		}
	}
}

// Musaarr imports what Lidarr cannot follow, so 24-bit wins as it would in Lidarr.
func TestPickForced24BitBeforeReadable(t *testing.T) {
	rels := []Release{
		rel("Kanye West - 2013 - Yeezus [FLAC - 44.1 kHz / 16 bits]", qFLAC, 11, "Wrong album"),
		rel("Kanye.West.Yeezus.2013.WEB.FLAC.24bit-GRP", qFLAC24, 2, "Unable to parse release"),
	}
	if got, _, _ := pick(rels, ye, "Yeezus", hifi); got.Title != rels[1].Title {
		t.Fatalf("got %q", got.Title)
	}
}

// Among Lidarr's 24-bit choices, the highest sample rate wins over Lidarr's own order (tracker, seeders).
func TestPickApprovedHighestRate(t *testing.T) {
	a := rel("Bon Iver - Bon Iver (2011) [FLAC 24bit 44.1kHz]", qFLAC24, 80)
	b := rel("Bon.Iver.Bon.Iver.2011.FLAC.24bit.96kHz-GRP", qFLAC24, 3)
	c := rel("Bon Iver - Bon Iver (2011) [FLAC 16bit 44.1kHz]", qFLAC, 200)
	a.ReleaseWeight, b.ReleaseWeight, c.ReleaseWeight = 1, 2, 3
	if got, _, _ := pick([]Release{c, a, b}, []string{"Bon Iver"}, "Bon Iver", hifi); got.Title != b.Title {
		t.Fatalf("got %q, want the 96 kHz one", got.Title)
	}
}

// A higher rate never puts a 16-bit source before a 24-bit one.
func TestPickApprovedBitsBeforeRate(t *testing.T) {
	a := rel("Album [FLAC 24bit 48kHz]", qFLAC24, 5)
	b := rel("Album [FLAC 16bit 96kHz]", qFLAC, 50)
	a.ReleaseWeight, b.ReleaseWeight = 1, 2
	if got, _, _ := pick([]Release{b, a}, ye, "x", hifi); got.Title != a.Title {
		t.Fatalf("got %q, want the 24-bit one", got.Title)
	}
}

func TestPickForcedHighestRate(t *testing.T) {
	rels := []Release{
		rel("Kanye.West.Donda.2021.WEB.FLAC.24bit.48kHz-A", qFLAC24, 40, "Unable to parse release"),
		rel("Kanye.West.Donda.2021.WEB.FLAC.24bit.96kHz-B", qFLAC24, 2, "Unable to parse release"),
	}
	if got, _, _ := pick(rels, ye, "Donda", hifi); got.Title != rels[1].Title {
		t.Fatalf("got %q", got.Title)
	}
}

// Past 96 kHz the rate no longer decides: Lidarr's order does, then the seeders.
func TestPickRateCappedAt96(t *testing.T) {
	a := rel("Album (2011) [FLAC 24bit 192kHz]", qFLAC24, 4)
	b := rel("Album (2011) [FLAC 24bit 96kHz]", qFLAC24, 60)
	a.ReleaseWeight, b.ReleaseWeight = 2, 1
	if got, _, _ := pick([]Release{a, b}, ye, "x", hifi); got.Title != b.Title {
		t.Fatalf("approved: got %q, want Lidarr's first, the 96 kHz one", got.Title)
	}
	rels := []Release{
		rel("Kanye.West.Donda.2021.WEB.FLAC.24bit.192kHz-A", qFLAC24, 2, "Unable to parse release"),
		rel("Kanye.West.Donda.2021.WEB.FLAC.24bit.96kHz-B", qFLAC24, 40, "Unable to parse release"),
	}
	if got, _, _ := pick(rels, ye, "Donda", hifi); got.Title != rels[1].Title {
		t.Fatalf("readable: got %q, want the better seeded one", got.Title)
	}
}
