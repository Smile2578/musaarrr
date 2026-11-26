package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// The server says what the page says: its English sentences are the page's (web/i18n/en.json),
// and an error or a note carries its key so the page shows it in the reader's language.

var englishWords = sync.OnceValue(func() map[string]string {
	var words map[string]string
	b, err := webFS.ReadFile("web/i18n/en.json")
	if err == nil {
		err = json.Unmarshal(b, &words)
	}
	if err != nil {
		log.Printf("i18n: %v", err)
	}
	return words
})

var holeRe = regexp.MustCompile(`\{(\w+)\}`)

// english is the English sentence for key, its {holes} filled from params; the key itself when unknown.
func english(key string, params map[string]any) string {
	s, ok := englishWords()[key]
	if !ok {
		return key
	}
	return holeRe.ReplaceAllStringFunc(s, func(hole string) string {
		if v, ok := params[hole[1:len(hole)-1]]; ok {
			return fmt.Sprint(v)
		}
		return hole
	})
}

// noteView reads a request's note: a code, "profile:<name>", or a sentence saved by an older
// version (in French), passed on as it is.
func noteView(note string) (text, code string, params map[string]any) {
	code, name, _ := strings.Cut(note, ":")
	if _, ok := englishWords()["note."+code]; !ok {
		return note, "", nil
	}
	if name != "" {
		params = map[string]any{"name": name}
	}
	return english("note."+code, params), code, params
}

// fail answers an error the page shows in the reader's language (key "error."+code);
// the English sentence goes along for API clients.
func fail(w http.ResponseWriter, status int, code string, params map[string]any) {
	writeErrCode(w, status, code, english("error."+code, params), params)
}
