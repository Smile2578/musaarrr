package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fake is a scripted HTTP backend that records every call it receives.
type fake struct {
	mu     sync.Mutex
	calls  []string // "METHOD /path?query"
	bodies map[string][]string
	routes map[string]func(w http.ResponseWriter, r *http.Request, body string)
	srv    *httptest.Server
}

func newFake(t *testing.T) *fake {
	f := &fake{bodies: map[string][]string{}, routes: map[string]func(http.ResponseWriter, *http.Request, string){}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		f.mu.Lock()
		f.calls = append(f.calls, key+"?"+r.URL.RawQuery)
		f.bodies[key] = append(f.bodies[key], string(b))
		h := f.routes[key]
		f.mu.Unlock()
		if r.Header.Get("X-Api-Key") != "secret-key" && r.Header.Get("X-Plex-Token") != "plex-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r, string(b))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) handle(key string, h func(w http.ResponseWriter, r *http.Request, body string)) {
	f.mu.Lock()
	f.routes[key] = h
	f.mu.Unlock()
}

func (f *fake) reply(key string, code int, body string) {
	f.handle(key, func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		io.WriteString(w, body)
	})
}

func (f *fake) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}
