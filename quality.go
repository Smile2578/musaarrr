package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

// Quality is what a release title says about its files; empty fields mean the title does not say.
type Quality struct {
	Format   string   `json:"format"`
	Bits     int      `json:"bits"`
	KHz      string   `json:"khz"`
	Layout   string   `json:"layout"` // "tracks", "image" (one file + .cue) or ""
	Flags    []string `json:"flags"`  // codes the page words (flag.<code>), or a name as it is (MQA)
	Lossless bool     `json:"lossless"`
}

// formats in order of preference: a "FLAC + MP3" pack is a FLAC release.
var formats = []struct {
	name     string
	re       *regexp.Regexp
	lossless bool
}{
	{"FLAC", regexp.MustCompile(`(?i)\bflac\b`), true},
	{"ALAC", regexp.MustCompile(`(?i)\balac\b`), true},
	{"WavPack", regexp.MustCompile(`(?i)\bwavpack\b|\bWV\b`), true},
	{"APE", regexp.MustCompile(`\bAPE\b`), true}, // upper case only: "ape" is a word too
	{"DSD", regexp.MustCompile(`\b(DSD(64|128|256|512)?|DSF|DFF)\b`), true},
	{"ISO", regexp.MustCompile(`\bISO\b|\bSACD\b`), true},
	{"MP3", regexp.MustCompile(`(?i)\bmp3\b`), false},
	{"AAC", regexp.MustCompile(`(?i)\b(aac|m4a)\b`), false},
	{"OGG", regexp.MustCompile(`(?i)\b(ogg|vorbis)\b`), false},
	{"MP3", regexp.MustCompile(`(?i)\b\d{3} ?kbps\b`), false}, // a bitrate alone: MP3 on these trackers
}

var (
	bitsRe  = regexp.MustCompile(`(?i)\b(16|24)[ ._-]?bits?\b`)
	pairRe  = regexp.MustCompile(`\b(16|24)[/-](44|48|88|96|176|192)\b`) // 24/96, 16-44
	khzRe   = regexp.MustCompile(`(?i)\b(\d{2,3})(?:[ .,](\d))?\s?khz\b`)
	imageRe = regexp.MustCompile(`(?i)\(image\s?\+`)
	trackRe = regexp.MustCompile(`(?i)\(tracks\b`)
	flagRes = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"discography", regexp.MustCompile(`(?i)дискография|discograph|intégrale|integrale|коллекция`)},
		// « [LP] », « (LP, 1959) », « 2LP », « 2 LP's », « [2xLP] », a vinyl in brackets: « Vinyl Days » and
		// « The Marshall Mathers LP » are albums
		{"vinyl", regexp.MustCompile(`(?i)[\[(]\d?LP\b|\b\d ?x? ?LP(?:'?s)?\b|[\[(][^\])]*\bvinyle?\b[^\])]*[\])]|\b(vinyl|LP)[ -]?rip\b`)},
		{"MQA", regexp.MustCompile(`\bMQA\b`)},
		{"multichannel", regexp.MustCompile(`(?i)\b5\.1\b|multichannel|multicanal`)},
	}
	fullKHz = map[string]string{"44": "44.1", "88": "88.2", "176": "176.4"}
)

// titleTags are the tags some indexers write in release titles, from the settings: HIRES_TAGS
// mark a 24-bit lossless release, MULTICHANNEL_TAGS a surround mix. None is built in.
var titleTags atomic.Pointer[[2][]string]

func setTitleTags(hiRes, multichannel string) {
	split := func(list string) (tags []string) {
		for tag := range strings.SplitSeq(list, ",") {
			if tag = strings.ToLower(strings.TrimSpace(tag)); tag != "" {
				tags = append(tags, tag)
			}
		}
		return tags
	}
	titleTags.Store(&[2][]string{split(hiRes), split(multichannel)})
}

func tagged(title string, tags []string) bool {
	title = strings.ToLower(title)
	return slices.ContainsFunc(tags, func(tag string) bool { return strings.Contains(title, tag) })
}

func ReadQuality(title string) Quality {
	var tags [2][]string
	if p := titleTags.Load(); p != nil {
		tags = *p
	}
	q := Quality{Flags: []string{}}
	for _, f := range formats {
		if f.re.MatchString(title) {
			q.Format, q.Lossless = f.name, f.lossless
			break
		}
	}
	if tagged(title, tags[0]) {
		q.Bits = 24
		if q.Format == "" {
			q.Format, q.Lossless = "FLAC", true
		}
	}
	if m := bitsRe.FindStringSubmatch(title); m != nil {
		q.Bits = atoi(m[1])
	}
	if m := khzRe.FindStringSubmatch(title); m != nil {
		q.KHz = m[1]
		if m[2] != "" {
			q.KHz += "." + m[2]
		}
	}
	if m := pairRe.FindStringSubmatch(title); m != nil {
		q.Bits = atoi(m[1])
		if q.KHz == "" {
			q.KHz = m[2]
			if full, ok := fullKHz[m[2]]; ok {
				q.KHz = full
			}
		}
	}
	switch {
	case imageRe.MatchString(title):
		q.Layout = "image"
	case trackRe.MatchString(title):
		q.Layout = "tracks"
	}
	for _, f := range flagRes {
		if f.re.MatchString(title) || f.name == "multichannel" && tagged(title, tags[1]) {
			q.Flags = append(q.Flags, f.name)
		}
	}
	return q
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// Level ranks a release, 0 best: 24 bits, 16 bits (tracks, or an image + cue that musictag
// cuts into tracks), vinyl or MQA (a hi-fi Lidarr profile scores them -10000), ISO or DSD (musictag cannot read
// them), unknown format, lossy.
func (q Quality) Level() int {
	switch {
	case q.Format == "":
		return 5
	case !q.Lossless:
		return 6
	case q.Format == "ISO" || q.Format == "DSD":
		return 4
	case slices.Contains(q.Flags, "vinyl") || slices.Contains(q.Flags, "MQA"):
		return 2
	case q.Bits == 24:
		return 0
	}
	return 1
}

// Rate is the sample rate in kHz the title gives, 0 when it gives none.
func (q Quality) Rate() float64 {
	r, _ := strconv.ParseFloat(q.KHz, 64)
	return r
}
