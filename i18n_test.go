package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	pluralForm = regexp.MustCompile(`_(zero|one|two|few|many|other)$`)
	hole       = regexp.MustCompile(`\{(\w+)\}`)
)

func catalogue(t *testing.T, lang string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("web", "i18n", lang+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]string
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s.json: %v", lang, err)
	}
	return c
}

// sentences groups a catalogue by key without its plural form: "albums.count_one" is "albums.count".
func sentences(c map[string]string) map[string][]string {
	out := map[string][]string{}
	for k, v := range c {
		base := pluralForm.ReplaceAllString(k, "")
		for _, m := range hole.FindAllStringSubmatch(v, -1) {
			if !slices.Contains(out[base], m[1]) {
				out[base] = append(out[base], m[1])
			}
		}
		if _, ok := out[base]; !ok {
			out[base] = nil
		}
	}
	return out
}

// Every language says every sentence, with the same holes, and has the other form of each count.
func TestCataloguesMatch(t *testing.T) {
	en := catalogue(t, "en")
	enS := sentences(en)
	for _, lang := range []string{"fr"} {
		c := catalogue(t, lang)
		cS := sentences(c)
		for k, holes := range enS {
			other, ok := cS[k]
			if !ok {
				t.Errorf("%s.json lacks %q", lang, k)
				continue
			}
			slices.Sort(holes)
			slices.Sort(other)
			if !slices.Equal(holes, other) {
				t.Errorf("%s.json %q has holes %v, en.json %v", lang, k, other, holes)
			}
		}
		for k := range cS {
			if _, ok := enS[k]; !ok {
				t.Errorf("%s.json has %q, which en.json lacks", lang, k)
			}
		}
		for k := range c {
			if base, form := pluralForm.ReplaceAllString(k, ""), pluralForm.FindString(k); form != "" {
				if _, ok := c[base+"_other"]; !ok {
					t.Errorf("%s.json has %q without %q", lang, k, base+"_other")
				}
			}
		}
	}
}

var (
	jsKey   = regexp.MustCompile(`\b(t|tn)\(\s*"([a-zA-Z0-9_.]+)"`)
	htmlKey = regexp.MustCompile(`data-i18n(?:-[a-z-]+)?="([a-zA-Z0-9_.]+)"`)
	goCode  = regexp.MustCompile(`(?:writeErrCode\([^,]+,[^,]+,|fail\([^,]+,[^,]+,|lidarrDown\([^,]+,|badSetup\()\s*"([a-zA-Z0-9_.]+)"`)
)

// Every key the page or the server names is in the English catalogue.
func TestKeysUsedExist(t *testing.T) {
	en := sentences(catalogue(t, "en"))
	check := func(file, key string) {
		if _, ok := en[key]; !ok {
			t.Errorf("%s uses %q, which en.json lacks", file, key)
		}
	}
	files, _ := filepath.Glob("web/*.js")
	more, _ := filepath.Glob("web/shop3d/*.js")
	pages, _ := filepath.Glob("web/*.html")
	for _, f := range append(append(files, more...), pages...) {
		if strings.HasPrefix(filepath.Base(f), "three") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range jsKey.FindAllStringSubmatch(string(b), -1) {
			check(f, m[2])
		}
		for _, m := range htmlKey.FindAllStringSubmatch(string(b), -1) {
			check(f, m[1])
		}
	}
	gos, _ := filepath.Glob("*.go")
	for _, f := range gos {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range goCode.FindAllStringSubmatch(string(b), -1) {
			check(f, "error."+m[1])
		}
	}
}

// The codes the server builds rather than names in a call have their sentence too, in English for API clients.
func TestServerCodesHaveWords(t *testing.T) {
	en := catalogue(t, "en")
	want := func(key string) {
		if _, ok := en[key]; !ok {
			t.Errorf("en.json lacks %q", key)
		}
	}
	want("error." + queryProblem("a"))
	want("error." + queryProblem(strings.Repeat("a", 101)))
	for _, err := range []error{ErrSourceExpired, ErrGrabUnsure, ErrAlreadySent, ErrSourceRefused, ErrProwlarrKey, context.DeadlineExceeded, errors.New("other")} {
		_, code := sourceErr(err)
		want("error." + code)
	}
	for _, err := range []error{context.DeadlineExceeded, errors.New("other")} {
		want("error." + lidarrDown(err, ""))
	}
	for _, code := range []string{unmonitoredNote, profileNote, notInPlexNote, removedNote, notReadyNote} {
		want("note." + code)
	}
	code := regexp.MustCompile(`^([a-z]+)(:\d+)?$`) // a name (SHM-CD, MQA) is shown as it is
	word := func(prefix, s string) {
		if m := code.FindStringSubmatch(s); m != nil {
			want(map[bool]string{false: prefix + "." + m[1], true: prefix + "." + m[1] + ".year"}[m[2] != ""])
		}
	}
	for _, f := range flagRes {
		word("flag", f.name)
	}
	hints := editionHints("Album (2006 Japan Expanded Anniversary Collector Box Mono Remastered 2013) Bonus", 1990)
	hints = append(hints, editionHints("Album Remastered", 0)...)
	if len(hints) < 10 {
		t.Fatalf("hints %v", hints)
	}
	for _, h := range hints {
		word("hint", h)
	}
}

func TestNoteView(t *testing.T) {
	if text, code, params := noteView(profileNote + ":Lossless"); text != "Artist's profile: Lossless" || code != "profile" || params["name"] != "Lossless" {
		t.Errorf("profile note = %q %q %v", text, code, params)
	}
	if text, code, _ := noteView("Album retiré de Lidarr."); text != "Album retiré de Lidarr." || code != "" { // saved by an older version
		t.Errorf("old note = %q %q", text, code)
	}
	if text, code, _ := noteView(""); text != "" || code != "" {
		t.Errorf("no note = %q %q", text, code)
	}
}

func TestFailSpeaksEnglishWithItsCode(t *testing.T) {
	w := httptest.NewRecorder()
	fail(w, 502, "request.refused", map[string]any{"detail": "Album already exists"})
	var body struct{ Error, Code string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 502 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if body.Code != "request.refused" || body.Error != "Lidarr turned the request down: Album already exists" {
		t.Errorf("got %+v", body)
	}
}
