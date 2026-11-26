package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sendZip posts body as a backup archive.
func sendZip(h http.Handler, body []byte, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/settings/restore", bytes.NewReader(body))
	r.RemoteAddr = lan
	r.Header.Set("Content-Type", "application/zip")
	r.AddCookie(c)
	return do(h, r)
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipNames(t *testing.T, b []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return names
}

func TestBackupHoldsAccountsSettingsAndRequests(t *testing.T) {
	f := fakeLidarr(t)
	core, h := coreApp(t, []string{"LIDARR_URL=" + f.srv.URL, "LIDARR_API_KEY=secret-key", "QUALITY_PROFILE=Lossless", "ROOT_FOLDER=/music"})
	c := admin(t, h)
	core.store.Put(Request{MBID: mbid, Title: "Discovery", Status: "in_plex"})
	os.WriteFile(filepath.Join(core.dir, "room.json"), []byte(`{}`), 0o600)
	w := send(h, "GET", "/api/settings/backup", "", c)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" ||
		!strings.HasPrefix(w.Header().Get("Content-Disposition"), `attachment; filename="musaarr-backup-`) {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if got := strings.Join(zipNames(t, w.Body.Bytes()), ","); got != "auth.json,requests.json,room.json,settings.json" {
		t.Fatalf("archive holds %s", got)
	}
}

// A backup moves an install: the new one gets its accounts, settings and requests, at once.
func TestRestoreMovesAnInstall(t *testing.T) {
	f := fakeLidarr(t)
	old, oldH := coreApp(t, nil)
	oc := sessionOf(t, post(oldH, "/api/auth/setup", `{"name":"alice","password":"long enough"}`, lan, nil))
	body := `{"values":{"LIDARR_URL":"` + f.srv.URL + `","LIDARR_API_KEY":"secret-key","QUALITY_PROFILE":"Lossless","ROOT_FOLDER":"/music"}}`
	if w := send(oldH, "PUT", "/api/settings", body, oc); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
	old.store.Put(Request{MBID: mbid, Title: "Discovery", Status: "in_plex"})
	backup := send(oldH, "GET", "/api/settings/backup", "", oc).Body.Bytes()

	_, h := coreApp(t, nil)
	bob := sessionOf(t, post(h, "/api/auth/setup", `{"name":"bob","password":"long enough"}`, lan, nil))
	if w := sendZip(h, backup, bob); w.Code != 200 {
		t.Fatalf("restore: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/requests", "", bob); w.Code != 401 {
		t.Fatalf("bob's account went with the old accounts: %d", w.Code)
	}
	alice := sessionOf(t, post(h, "/api/auth/login", `{"name":"alice","password":"long enough"}`, lan, nil))
	w := send(h, "GET", "/api/requests", "", alice)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Discovery") {
		t.Fatalf("requests after restore: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/settings", "", alice); !strings.Contains(w.Body.String(), `"ready":true`) {
		t.Fatalf("settings after restore: %s", w.Body)
	}
}

func TestRestoreRefusesABadArchive(t *testing.T) {
	_, h := coreApp(t, nil)
	c := admin(t, h)
	noAdmin := `{"users":[{"id":1,"name":"x","hash":"h","admin":false}]}`
	cases := map[string][]byte{
		"backup.unreadable": []byte("not a zip"),
		"backup.empty":      zipOf(t, map[string]string{"notes.txt": "hello"}),
		"backup.badFile":    zipOf(t, map[string]string{"requests.json": "{oops"}),
		"backup.noAdmin":    zipOf(t, map[string]string{"auth.json": noAdmin}),
		"backup.tooNew":     zipOf(t, map[string]string{"settings.json": `{"version":99,"values":{}}`}),
	}
	for code, body := range cases {
		if w := sendZip(h, body, c); w.Code != 400 || errCode(w) != code {
			t.Errorf("%s: %d %s", code, w.Code, w.Body)
		}
	}
	if w := send(h, "GET", "/api/settings", "", c); w.Code != 200 {
		t.Fatalf("a refused restore changed the accounts: %d", w.Code)
	}
}

// A restore never locks out the admin ALLOWED_LOGIN names: the proxy still lets them in.
func TestRestoreKeepsTheProxyAdmin(t *testing.T) {
	a, err := OpenAuth(t.TempDir(), "Tailscale-User-Login", "192.0.2.0/24", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.replace(authFile{Users: []User{{ID: 1, Name: "alice", Hash: hashPassword("long enough"), Admin: true}}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/auth", nil)
	r.RemoteAddr = "192.0.2.1:443"
	r.Header.Set("Tailscale-User-Login", "owner@example.com")
	if u, proxy, ok := a.User(r); !ok || !proxy || !u.Admin {
		t.Fatalf("the proxy admin after a restore: %+v %v %v", u, proxy, ok)
	}
}

// A backup is held to the page's rules: web addresses only, and a key fixed by the environment
// stays with its address.
func TestRestoreKeepsThePageRules(t *testing.T) {
	_, h := coreApp(t, []string{"PROWLARR_API_KEY=p-key"})
	c := admin(t, h)
	cases := map[string]string{
		"backup.badFile":      `{"version":1,"values":{"METADATA_URL":"http://u:p@meta.example"}}`,
		"setup.addressLocked": `{"version":1,"values":{"PROWLARR_URL":"http://elsewhere.example"}}`,
	}
	for code, settings := range cases {
		if w := sendZip(h, zipOf(t, map[string]string{"settings.json": settings}), c); w.Code != 400 || errCode(w) != code {
			t.Errorf("%s: %d %s", code, w.Code, w.Body)
		}
	}
	if w := sendZip(h, zipOf(t, map[string]string{"settings.json": `{"version":1,"values":{}}`}), c); w.Code != 200 {
		t.Fatalf("the same address: %d %s", w.Code, w.Body)
	}
}

// A name held twice is refused: each file is read once, whatever the archive says.
func TestRestoreRefusesAFileTwice(t *testing.T) {
	_, h := coreApp(t, nil)
	c := admin(t, h)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for range 2 {
		w, _ := zw.Create("requests.json")
		io.WriteString(w, "[]")
	}
	zw.Close()
	if w := sendZip(h, buf.Bytes(), c); w.Code != 400 || errCode(w) != "backup.unreadable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
