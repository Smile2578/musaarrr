package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSourcesOrder(t *testing.T) {
	withTitleTags(t, "[HR24]", "[MC]")
	_, p := newTestProwlarr(t)
	items, err := NewSources(p).Search(ctx, "Bon Iver", time.Now())
	if err != nil || len(items) != 19 {
		t.Fatalf("got %d, %v", len(items), err)
	}
	for i, prefix := range []string{"[HR24][OF][LDR] Bon Iver - Sable", "(Indie Folk | Rock) [WEB] Bon Iver - Sable", "(Indie Folk) [CD] Bon Iver - i,i"} {
		if !strings.HasPrefix(items[i].Title, prefix) {
			t.Errorf("item %d: %s", i, items[i].Title)
		}
	}
	if last := items[len(items)-1]; last.Seeders != 0 || !strings.HasPrefix(last.Title, "VA.Rockin.Vibes") {
		t.Errorf("last %s (%d seeders)", last.Title, last.Seeders)
	}
}

func TestSourcesHideGUID(t *testing.T) {
	_, p := newTestProwlarr(t)
	items, _ := NewSources(p).Search(ctx, "Bon Iver", time.Now())
	b, _ := json.Marshal(items)
	for _, leak := range []string{"REDACTED", "guid", "indexerId"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("%s reaches the browser", leak)
		}
	}
	hex := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for _, it := range items {
		if !hex.MatchString(it.Token) || seen[it.Token] {
			t.Fatalf("token %q", it.Token)
		}
		seen[it.Token] = true
	}
}

func TestSourcesDropAudiobooks(t *testing.T) {
	f, p := newTestProwlarr(t)
	f.reply("GET /api/v1/search", 200, `[{"guid":"a","indexerId":1,"indexer":"X","title":"Livre audio","categories":[{"id":3030}]},
	 {"guid":"b","indexerId":1,"indexer":"X","title":"Album FLAC","seeders":3,"categories":[{"id":3040}]}]`)
	items, _ := NewSources(p).Search(ctx, "x", time.Now())
	if len(items) != 1 || items[0].Title != "Album FLAC" {
		t.Fatalf("got %+v", items)
	}
}

func TestSourcesGrabOnce(t *testing.T) {
	f, p := newTestProwlarr(t)
	s := NewSources(p)
	now := time.Now()
	items, _ := s.Search(ctx, "Bon Iver", now)
	title, indexer, err := s.Grab(ctx, items[0].Token, now)
	if err != nil || title != items[0].Title || indexer != "Indexer B" {
		t.Fatalf("got %q %q %v", title, indexer, err)
	}
	if b := f.bodies["POST /api/v1/search"]; len(b) != 1 || b[0] != `{"guid":"REDACTED","indexerId":3}` {
		t.Fatalf("body %v", b)
	}
	if _, _, err := s.Grab(ctx, items[0].Token, now); !errors.Is(err, ErrAlreadySent) {
		t.Fatalf("second grab: %v", err)
	}
	if n := f.count("POST /api/v1/search"); n != 1 {
		t.Fatalf("%d grabs sent", n)
	}
}

func TestSourcesGrabRetryAfterFailure(t *testing.T) {
	f, p := newTestProwlarr(t)
	s := NewSources(p)
	items, _ := s.Search(ctx, "Bon Iver", time.Now())
	f.reply("POST /api/v1/search", 409, `{}`)
	if _, _, err := s.Grab(ctx, items[0].Token, time.Now()); !errors.Is(err, ErrSourceRefused) {
		t.Fatalf("got %v", err)
	}
	f.reply("POST /api/v1/search", 200, `{}`)
	if _, _, err := s.Grab(ctx, items[0].Token, time.Now()); err != nil {
		t.Fatalf("a failed grab must stay retryable: %v", err)
	}
}

func TestSourcesTokenExpires(t *testing.T) {
	f, p := newTestProwlarr(t)
	s := NewSources(p)
	now := time.Now()
	items, _ := s.Search(ctx, "Bon Iver", now)
	if _, _, err := s.Grab(ctx, items[0].Token, now.Add(tokenTTL+time.Second)); !errors.Is(err, ErrSourceExpired) {
		t.Fatalf("expired: %v", err)
	}
	if _, _, err := s.Grab(ctx, strings.Repeat("ab", 16), now); !errors.Is(err, ErrSourceExpired) {
		t.Fatalf("unknown: %v", err)
	}
	if f.count("POST") != 0 {
		t.Fatal("expired token reached Prowlarr")
	}
}

// A grab Prowlarr does not answer in time may still reach qBittorrent: a second tap must not send it again.
func TestSourcesGrabTimeoutKeepsClaim(t *testing.T) {
	f, p := newTestProwlarr(t)
	s := NewSources(p)
	items, _ := s.Search(ctx, "Bon Iver", time.Now())
	f.handle("POST /api/v1/search", func(w http.ResponseWriter, r *http.Request, _ string) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	})
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	title, _, err := s.Grab(short, items[0].Token, time.Now())
	if !errors.Is(err, ErrGrabUnsure) || title != items[0].Title {
		t.Fatalf("got %q %v", title, err)
	}
	if _, _, err := s.Grab(ctx, items[0].Token, time.Now()); !errors.Is(err, ErrGrabUnsure) {
		t.Fatalf("second tap: %v", err)
	}
	if n := f.count("POST /api/v1/search"); n != 1 {
		t.Fatalf("%d grabs sent", n)
	}
}

// Prowlarr down: nothing was sent, so the same result can be tried again.
func TestSourcesGrabUnreachableStaysRetryable(t *testing.T) {
	f, p := newTestProwlarr(t)
	s := NewSources(p)
	items, _ := s.Search(ctx, "Bon Iver", time.Now())
	f.srv.Close()
	if _, _, err := s.Grab(ctx, items[0].Token, time.Now()); err == nil || errors.Is(err, ErrGrabUnsure) {
		t.Fatalf("got %v", err)
	}
	if _, _, err := s.Grab(ctx, items[0].Token, time.Now()); errors.Is(err, ErrAlreadySent) || errors.Is(err, ErrGrabUnsure) {
		t.Fatalf("second try refused: %v", err)
	}
}

func TestSourcesConcurrentGrab(t *testing.T) {
	f, p := newTestProwlarr(t)
	s := NewSources(p)
	items, _ := s.Search(ctx, "Bon Iver", time.Now())
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { s.Grab(ctx, items[0].Token, time.Now()) })
	}
	wg.Wait()
	if n := f.count("POST /api/v1/search"); n != 1 {
		t.Fatalf("%d grabs sent", n)
	}
}

// Each token maps to its own release.
func TestSourcesTokenMapsToItsRelease(t *testing.T) {
	f, p := newTestProwlarr(t)
	f.reply("GET /api/v1/search", 200, `[{"guid":"g-flac","indexerId":3,"indexer":"Indexer B","title":"A - B (2020) [FLAC 24bit]","seeders":5,"categories":[{"id":3040}]},
	 {"guid":"g-mp3","indexerId":2,"indexer":"Indexer A","title":"A - B (2020) [MP3 320]","seeders":9,"categories":[{"id":3010}]}]`)
	s := NewSources(p)
	items, _ := s.Search(ctx, "A B", time.Now())
	if len(items) != 2 || items[1].Indexer != "Indexer A" {
		t.Fatalf("got %+v", items)
	}
	s.Grab(ctx, items[1].Token, time.Now())
	if b := f.bodies["POST /api/v1/search"]; len(b) != 1 || b[0] != `{"guid":"g-mp3","indexerId":2}` {
		t.Fatalf("body %v", b)
	}
}

// An indexer answers « Bon Iver » with Taylor Swift too: releases named after the search come first.
func TestSourcesMatchFirst(t *testing.T) {
	_, p := newTestProwlarr(t)
	items, _ := NewSources(p).Search(ctx, "bon iver", time.Now())
	firstOther := slices.IndexFunc(items, func(it SourceItem) bool { return !it.Match })
	if firstOther < 1 {
		t.Fatalf("no match first: %d", firstOther)
	}
	for i, it := range items {
		swift := strings.Contains(it.Title, "Taylor")
		boniver := strings.Contains(strings.ReplaceAll(strings.ToLower(it.Title), ".", " "), "bon iver")
		if (swift && it.Match) || (boniver && !it.Match) || (i < firstOther) != it.Match {
			t.Errorf("%d %v %s", i, it.Match, it.Title)
		}
	}
	if !sourceMatches(words("Sigur Rós ( )"), "Sigur.Ros.-.( ).2002.FLAC") || !sourceMatches(words("bon iver"), "Bon.Iver.For.Emma.2008.FLAC") {
		t.Error("accents or dots break the match")
	}
}

// Whole words only: « Bon Iver » is not in « Boney M - Rivers of Babylon » (seen on the NAS, v1.3.1).
func TestSourceMatchesWholeWords(t *testing.T) {
	for _, c := range []struct {
		q, title string
		want     bool
	}{
		{"bon iver", "Boney M - Rivers of Babylon (1978) [FLAC]", false},
		{"bon iver", "James Bond 50th Anniversary - 2012, FLAC", false},
		{"bon iver", "Bon.Iver.For.Emma.Forever.Ago.2008.FLAC", true},
		{"fred again", "Fred again.. - Actual Life 3 (2022) [FLAC]", true},
		{"acdc", "AC/DC - Back in Black (1980) [FLAC 24-96]", true},
		{"boniver", "Bon Iver - i,i - 2019, FLAC (image+.cue)", true},
		{"Sigur Rós", "Sigur.Ros.-.( ).2002.FLAC", true},
	} {
		if got := sourceMatches(words(c.q), c.title); got != c.want {
			t.Errorf("%q in %q: %v", c.q, c.title, got)
		}
	}
}

func TestSourcesFiles(t *testing.T) {
	f, p := newTestProwlarr(t)
	f.reply("GET /api/v1/search", 200, `[{"guid":"g","indexerId":1,"indexer":"X","title":"Late Registration","seeders":3,"categories":[{"id":3040}],
	 "downloadUrl":"http://prowlarr.lan/1/download?apikey=secret-key&link=xyz"}]`)
	f.handle("GET /1/download", func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Write(multiTorrent("Late Registration/01 Wake Up Mr. West.flac", "Late Registration/folder.jpg"))
	})
	s := NewSources(p)
	now := time.Now()
	items, _ := s.Search(ctx, "late registration", now)
	files, err := s.Files(ctx, items[0].Token, now)
	if err != nil || len(files) != 2 || files[0].Path[1] != "01 Wake Up Mr. West.flac" {
		t.Fatalf("got %+v %v", files, err)
	}
	if _, err := s.Files(ctx, items[0].Token, now); err != nil || f.count("GET /1/download") != 1 {
		t.Fatalf("second read fetched again: %v, %d", err, f.count("GET /1/download"))
	}
	if _, err := s.Files(ctx, items[0].Token, now.Add(tokenTTL+time.Second)); !errors.Is(err, ErrSourceExpired) {
		t.Fatalf("expired: %v", err)
	}
	if _, err := s.Files(ctx, "nope", now); !errors.Is(err, ErrSourceExpired) {
		t.Fatalf("unknown: %v", err)
	}
}

// At the same level, tracks come before an image + cue: musictag must cut the image first, and its tracks cannot be matched to an edition.
func TestSourceOrderTracksBeforeImage(t *testing.T) {
	image := SourceItem{Title: "image", Match: true, Seeders: 30, Quality: ReadQuality("A - B, FLAC (image+.cue)")}
	tracks := SourceItem{Title: "tracks", Match: true, Seeders: 3, Quality: ReadQuality("A - B, FLAC (tracks)")}
	if sourceOrder(image, tracks) <= 0 {
		t.Fatal("image + cue ranked before tracks")
	}
}

// At the same level, the higher sample rate comes first, then a known rate before an unknown one.
func TestSourceOrderHigherRateFirst(t *testing.T) {
	withTitleTags(t, "[HR24]", "")
	r48 := SourceItem{Title: "48", Match: true, Seeders: 40, Quality: ReadQuality("A - B - 2020, FLAC (tracks), 24 bit/48 kHz")}
	r96 := SourceItem{Title: "96", Match: true, Seeders: 2, Quality: ReadQuality("A - B - 2020 [24/96] FLAC (tracks)")}
	r192 := SourceItem{Title: "192", Match: true, Seeders: 1, Quality: ReadQuality("A.B.2020.FLAC.24bit.192kHz-GRP")}
	unknown := SourceItem{Title: "?", Match: true, Seeders: 90, Quality: ReadQuality("[HR24][OF] A - B - 2020")}
	cd := SourceItem{Title: "16", Match: true, Seeders: 9, Quality: ReadQuality("A.B.2020.FLAC.16bit.96kHz-GRP")}
	items := []SourceItem{cd, unknown, r48, r96, r192}
	slices.SortStableFunc(items, sourceOrder)
	var got []string
	for _, it := range items {
		got = append(got, it.Title)
	}
	if want := []string{"192", "96", "48", "?", "16"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
