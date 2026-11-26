package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakePlexTV is plex.tv for one PIN (42, code ABCD) that the test signs in with signIn.
func fakePlexTV(t *testing.T, server string) (tv *httptest.Server, signIn func()) {
	var done atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/pins", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Client-Identifier") == "" || r.Header.Get("X-Plex-Product") != "Musaarr" || r.URL.Query().Get("strong") != "true" {
			http.Error(w, "who are you", 400)
			return
		}
		w.Write([]byte(`{"id":42,"code":"ABCD"}`))
	})
	mux.HandleFunc("GET /api/v2/pins/42", func(w http.ResponseWriter, r *http.Request) {
		if done.Load() {
			w.Write([]byte(`{"id":42,"code":"ABCD","authToken":"account-token"}`))
			return
		}
		w.Write([]byte(`{"id":42,"code":"ABCD","authToken":null}`))
	})
	mux.HandleFunc("GET /api/v2/resources", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "account-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		w.Write([]byte(`[{"name":"Living room","clientIdentifier":"abc","provides":"server","accessToken":"plex-token",
		  "connections":[{"uri":"http://127.0.0.1:1","local":true},{"uri":"` + server + `","local":true}]},
		 {"name":"Phone","clientIdentifier":"p1","provides":"client,player","connections":[]}]`))
	})
	tv = httptest.NewServer(mux)
	t.Cleanup(tv.Close)
	return tv, func() { done.Store(true) }
}

func TestPlexSignInByPin(t *testing.T) {
	plex := newFake(t)
	plex.reply("GET /identity", 200, `{"MediaContainer":{"machineIdentifier":"abc"}}`)
	tv, signIn := fakePlexTV(t, plex.srv.URL)
	core, h := coreApp(t, nil)
	core.plexTV = tv.URL
	c := admin(t, h)

	w := send(h, "POST", "/api/settings/plex/pin", "", c)
	var pin struct {
		ID   int    `json:"id"`
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &pin)
	if w.Code != 200 || pin.ID != 42 || pin.Code != "ABCD" || !strings.HasPrefix(pin.URL, "https://app.plex.tv/auth#?") || !strings.Contains(pin.URL, "code=ABCD") {
		t.Fatalf("pin: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/settings/plex/pin/42", "", c); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"done":false}` {
		t.Fatalf("before sign-in: %d %s", w.Code, w.Body)
	}
	signIn()
	w = send(h, "GET", "/api/settings/plex/pin/42", "", c)
	want := `{"done":true,"servers":[{"id":"abc","name":"Living room","connections":[{"uri":"` + plex.srv.URL + `","local":true,"ok":true},{"uri":"http://127.0.0.1:1","local":true}]}]}`
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("after sign-in: %d %s\nwant %s", w.Code, w.Body, want)
	}

	if w := send(h, "POST", "/api/settings/plex/server", `{"pin":42,"server":"abc","uri":"http://elsewhere:32400"}`, c); w.Code != 400 || errCode(w) != "setup.plexUnknownServer" {
		t.Fatalf("an address plex.tv did not give: %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/settings/plex/server", `{"pin":42,"server":"abc","uri":"`+plex.srv.URL+`"}`, c); w.Code != 200 {
		t.Fatalf("choose: %d %s", w.Code, w.Body)
	}
	w = send(h, "GET", "/api/settings", "", c)
	if !strings.Contains(w.Body.String(), `"PLEX_URL":{"value":"`+plex.srv.URL+`"}`) || !strings.Contains(w.Body.String(), `"PLEX_TOKEN":{"set":true}`) {
		t.Fatalf("saved: %s", w.Body)
	}
	b, _ := os.ReadFile(filepath.Join(core.dir, "settings.json"))
	if !strings.Contains(string(b), `"PLEX_TOKEN": "plex-token"`) || strings.Contains(string(b), "account-token") {
		t.Fatalf("settings.json keeps the server's token, not the account's: %s", b)
	}
}

func TestPlexPinNotOurs(t *testing.T) {
	tv, _ := fakePlexTV(t, "http://127.0.0.1:1")
	core, h := coreApp(t, nil)
	core.plexTV = tv.URL
	c := admin(t, h)
	if w := send(h, "GET", "/api/settings/plex/pin/42", "", c); w.Code != 404 || errCode(w) != "setup.plexPinUnknown" {
		t.Fatalf("a pin this Musaarr did not ask for: %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/settings/plex/server", `{"pin":42,"server":"abc","uri":"http://127.0.0.1:1"}`, c); w.Code != 404 || errCode(w) != "setup.plexPinUnknown" {
		t.Fatalf("choose without a pin: %d %s", w.Code, w.Body)
	}
}
