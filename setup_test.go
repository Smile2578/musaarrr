package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// coreApp is a whole Musaarr over dir: accounts, settings and the app once Lidarr is set.
func coreApp(t *testing.T, environ []string) (*Core, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	settings, err := OpenSettings(dir, environ)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := OpenAuth(dir, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore(dir, settings, auth, store)
	t.Cleanup(c.Stop)
	if err := c.Apply(); err != nil {
		t.Fatal(err)
	}
	return c, withSecurity(auth, c)
}

// admin creates the first account and returns its session.
func admin(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	return sessionOf(t, post(h, "/api/auth/setup", `{"name":"admin","password":"long enough"}`, lan, nil))
}

func send(h http.Handler, method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = lan
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if c != nil {
		r.AddCookie(c)
	}
	return do(h, r)
}

// fakeLidarr answers the wizard's questions; its key is "secret-key".
func fakeLidarr(t *testing.T) *fake {
	f := newFake(t)
	f.reply("GET /api/v1/system/status", 200, `{"appName":"Lidarr","version":"3.2.1.5196"}`)
	f.reply("GET /api/v1/qualityprofile", 200, `[{"id":1,"name":"Any"},{"id":4,"name":"Lossless"}]`)
	f.reply("GET /api/v1/metadataprofile", 200, `[{"id":1,"name":"Standard"},{"id":5,"name":"None"}]`)
	f.reply("GET /api/v1/rootfolder", 200, `[{"path":"/music","defaultQualityProfileId":4,"defaultMetadataProfileId":1}]`)
	return f
}

func TestFreshInstallOpensOnTheWizard(t *testing.T) {
	_, h := coreApp(t, nil)
	c := admin(t, h)
	if w := send(h, "GET", "/api/requests", "", c); w.Code != 503 || errCode(w) != "setup.required" {
		t.Fatalf("app before setup: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/", "", c); w.Code != 303 || w.Header().Get("Location") != "/setup.html" {
		t.Fatalf("home before setup: %d %v", w.Code, w.Header())
	}
	if w := send(h, "GET", "/setup.html", "", c); w.Code != 200 {
		t.Fatalf("wizard page: %d", w.Code)
	}
	if w := send(h, "GET", "/healthz", "", nil); w.Code != 200 {
		t.Fatalf("healthz during setup: %d", w.Code) // the container is fine, just not set up
	}
	w := send(h, "GET", "/api/settings", "", c)
	var s settingsView
	json.Unmarshal(w.Body.Bytes(), &s)
	if w.Code != 200 || s.Ready || s.Values["LIDARR_API_KEY"].Set || s.Values["METADATA_PROFILE"].Value != "None" {
		t.Fatalf("settings: %d %s", w.Code, w.Body)
	}
}

func TestWizardSetsLidarrUp(t *testing.T) {
	f := fakeLidarr(t)
	_, h := coreApp(t, nil)
	c := admin(t, h)
	w := send(h, "POST", "/api/settings/lidarr/test", `{"url":"`+f.srv.URL+`","apiKey":"secret-key"}`, c)
	var ch LidarrChoices
	json.Unmarshal(w.Body.Bytes(), &ch)
	if w.Code != 200 || ch.Version != "3.2.1.5196" || strings.Join(ch.QualityProfiles, ",") != "Any,Lossless" ||
		len(ch.RootFolders) != 1 || ch.RootFolders[0].QualityProfile != "Lossless" || ch.RootFolders[0].MetadataProfile != "Standard" {
		t.Fatalf("test: %d %s", w.Code, w.Body)
	}
	body := `{"values":{"LIDARR_URL":"` + f.srv.URL + `","LIDARR_API_KEY":"secret-key","QUALITY_PROFILE":"Lossless","ROOT_FOLDER":"/music"}}`
	if w := send(h, "PUT", "/api/settings", body, c); w.Code != 200 || !strings.Contains(w.Body.String(), `"ready":true`) {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/requests", "", c); w.Code != 200 {
		t.Fatalf("app after setup: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/settings", "", c); strings.Contains(w.Body.String(), "secret-key") || !strings.Contains(w.Body.String(), `"LIDARR_API_KEY":{"set":true`) {
		t.Fatalf("a key goes back to the browser: %s", w.Body)
	}
}

func TestLidarrTestSaysWhatIsWrong(t *testing.T) {
	f := fakeLidarr(t)
	radarr := newFake(t)
	radarr.reply("GET /api/v1/system/status", 200, `{"appName":"Radarr","version":"5"}`)
	hosts := newFake(t)
	hosts.reply("GET /api/v1/system/status", 400, `Bad Request - Invalid Hostname`)
	_, h := coreApp(t, nil)
	c := admin(t, h)
	cases := map[string]string{
		`{"url":"` + f.srv.URL + `","apiKey":"wrong"}`:           "setup.lidarrBadKey",
		`{"url":"http://127.0.0.1:1","apiKey":"secret-key"}`:     "setup.lidarrUnreachable",
		`{"url":"` + radarr.srv.URL + `","apiKey":"secret-key"}`: "setup.notLidarr",
		`{"url":"` + hosts.srv.URL + `","apiKey":"secret-key"}`:  "setup.lidarrHostRejected",
		`{"url":"ftp://x","apiKey":"secret-key"}`:                "setup.badUrl",
	}
	for body, code := range cases {
		if w := send(h, "POST", "/api/settings/lidarr/test", body, c); w.Code != 400 || errCode(w) != code {
			t.Errorf("%s: %d %s, want %s", body, w.Code, w.Body, code)
		}
	}
	// A profile Lidarr does not have is refused at save, and nothing is kept.
	body := `{"values":{"LIDARR_URL":"` + f.srv.URL + `","LIDARR_API_KEY":"secret-key","QUALITY_PROFILE":"Hi-Fi","ROOT_FOLDER":"/music"}}`
	if w := send(h, "PUT", "/api/settings", body, c); w.Code != 400 || errCode(w) != "setup.lidarrProfiles" {
		t.Fatalf("missing profile: %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/settings", "", c); strings.Contains(w.Body.String(), `"LIDARR_API_KEY":{"set":true`) {
		t.Fatal("a refused save was kept")
	}
}

// The saved key is used when the page sends none: it never has to travel back.
func TestLidarrTestReusesTheSavedKey(t *testing.T) {
	f := fakeLidarr(t)
	_, h := coreApp(t, []string{"LIDARR_URL=" + f.srv.URL, "LIDARR_API_KEY=secret-key"})
	c := admin(t, h)
	if w := send(h, "POST", "/api/settings/lidarr/test", `{"url":"`+f.srv.URL+`"}`, c); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestSettingsAdminOnly(t *testing.T) {
	core, h := coreApp(t, nil)
	admin(t, h)
	core.auth.mu.Lock()
	core.auth.users = append(core.auth.users, User{ID: 2, Name: "guest", Hash: hashPassword("long enough")})
	core.auth.mu.Unlock()
	guest := sessionOf(t, post(h, "/api/auth/login", `{"name":"guest","password":"long enough"}`, lan, nil))
	for _, p := range []string{"GET /api/settings", "PUT /api/settings", "POST /api/settings/lidarr/test", "POST /api/settings/plex/pin"} {
		method, path, _ := strings.Cut(p, " ")
		if w := send(h, method, path, `{}`, guest); w.Code != 403 || errCode(w) != "auth.adminOnly" {
			t.Errorf("%s: %d %s", p, w.Code, w.Body)
		}
	}
}

func TestSettingsFixedByTheEnvironment(t *testing.T) {
	f := fakeLidarr(t)
	_, h := coreApp(t, []string{"LIDARR_URL=" + f.srv.URL, "LIDARR_API_KEY=secret-key"})
	c := admin(t, h)
	w := send(h, "GET", "/api/settings", "", c)
	if !strings.Contains(w.Body.String(), `"LIDARR_API_KEY":{"set":true,"locked":true}`) || !strings.Contains(w.Body.String(), `"QUALITY_PROFILE":{"value":"Hi-Fi"}`) {
		t.Fatalf("view: %s", w.Body)
	}
	if w := send(h, "PUT", "/api/settings", `{"values":{"LIDARR_API_KEY":"other"}}`, c); w.Code != 409 || errCode(w) != "setup.locked" {
		t.Fatalf("change a fixed value: %d %s", w.Code, w.Body)
	}
	if w := send(h, "PUT", "/api/settings", `{"values":{"DATA_DIR":"/"}}`, c); w.Code != 400 || errCode(w) != "setup.unknownSetting" {
		t.Fatalf("unknown key: %d %s", w.Code, w.Body)
	}
}

// The saved key never goes to an address the page names: it could be anyone's.
func TestLidarrTestKeepsTheSavedKeyHome(t *testing.T) {
	f := fakeLidarr(t)
	other := fakeLidarr(t)
	_, h := coreApp(t, []string{"LIDARR_URL=" + f.srv.URL, "LIDARR_API_KEY=secret-key"})
	c := admin(t, h)
	if w := send(h, "POST", "/api/settings/lidarr/test", `{"url":"`+other.srv.URL+`"}`, c); w.Code != 400 || errCode(w) != "setup.lidarrBadKey" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// A new address for a saved key needs the key typed again: an admin session that is not the
// admin's own must not be able to send the saved key to a server of its choosing.
func TestSavedKeyNeverFollowsANewAddress(t *testing.T) {
	f := fakeLidarr(t)
	other := fakeLidarr(t)
	_, h := coreApp(t, nil)
	c := admin(t, h)
	body := `{"values":{"LIDARR_URL":"` + f.srv.URL + `","LIDARR_API_KEY":"secret-key","QUALITY_PROFILE":"Lossless","ROOT_FOLDER":"/music","PROWLARR_URL":"http://prowlarr.example","PROWLARR_API_KEY":"p-key"}}`
	if w := send(h, "PUT", "/api/settings", body, c); w.Code != 200 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	for _, change := range []string{
		`{"values":{"LIDARR_URL":"` + other.srv.URL + `"}}`,
		`{"values":{"PROWLARR_URL":"http://elsewhere.example"}}`,
	} {
		if w := send(h, "PUT", "/api/settings", change, c); w.Code != 400 || errCode(w) != "setup.keyAgain" {
			t.Errorf("%s: %d %s", change, w.Code, w.Body)
		}
	}
	if n := other.count("GET"); n != 0 {
		t.Fatalf("the other Lidarr was called %d times", n)
	}
	same := `{"values":{"LIDARR_URL":"` + f.srv.URL + `/"}}` // the same address
	if w := send(h, "PUT", "/api/settings", same, c); w.Code != 200 {
		t.Fatalf("unchanged addresses: %d %s", w.Code, w.Body)
	}
	moved := `{"values":{"LIDARR_URL":"` + other.srv.URL + `","LIDARR_API_KEY":"secret-key"}}`
	if w := send(h, "PUT", "/api/settings", moved, c); w.Code != 200 {
		t.Fatalf("a new address with its key: %d %s", w.Code, w.Body)
	}
}

func TestKeyFromTheEnvironmentPinsItsAddress(t *testing.T) {
	f := fakeLidarr(t)
	_, h := coreApp(t, []string{"LIDARR_API_KEY=secret-key"})
	c := admin(t, h)
	body := `{"values":{"LIDARR_URL":"` + f.srv.URL + `"}}`
	if w := send(h, "PUT", "/api/settings", body, c); w.Code != 409 || errCode(w) != "setup.addressLocked" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

// An address is a web address with no name or password in it: the page shows addresses.
func TestSettingsTakeOnlyWebAddresses(t *testing.T) {
	_, h := coreApp(t, nil)
	c := admin(t, h)
	for _, bad := range []string{"http://user:pass@prowlarr:9696", "ftp://prowlarr", "prowlarr:9696"} {
		w := send(h, "PUT", "/api/settings", `{"values":{"PROWLARR_URL":"`+bad+`"}}`, c)
		if w.Code != 400 || errCode(w) != "setup.badAddress" || strings.Contains(w.Body.String(), "user:pass") {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body)
		}
	}
	if w := send(h, "PUT", "/api/settings", `{"values":{"LISTENBRAINZ_URL":"off","PROWLARR_URL":""}}`, c); w.Code != 200 {
		t.Fatalf("off and empty: %d %s", w.Code, w.Body)
	}
}

// A Lidarr that redirects does not get its key sent on to where it points.
func TestLidarrKeyNotSentOnARedirect(t *testing.T) {
	other := fakeLidarr(t)
	f := newFake(t)
	f.handle("GET /api/v1/system/status", func(w http.ResponseWriter, r *http.Request, _ string) {
		http.Redirect(w, r, other.srv.URL+r.URL.Path, http.StatusFound)
	})
	_, h := coreApp(t, nil)
	c := admin(t, h)
	w := send(h, "POST", "/api/settings/lidarr/test", `{"url":"`+f.srv.URL+`","apiKey":"secret-key"}`, c)
	if w.Code != 400 || other.count("GET") != 0 {
		t.Fatalf("%d %s, %d calls followed", w.Code, w.Body, other.count("GET"))
	}
}

// The release-title tags are settings: the page saves them, and the app reads titles with them.
func TestTitleTagsAreSettings(t *testing.T) {
	_, h := coreApp(t, nil)
	c := admin(t, h)
	t.Cleanup(func() { setTitleTags("", "") })
	if w := send(h, "PUT", "/api/settings", `{"values":{"HIRES_TAGS":"[HR24]","MULTICHANNEL_TAGS":"[MC]"}}`, c); w.Code != 200 ||
		!strings.Contains(w.Body.String(), `"HIRES_TAGS":{"value":"[HR24]"}`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if q := ReadQuality("[HR24] A - B - 2020"); q.Bits != 24 {
		t.Fatalf("the saved tag is not read: %+v", q)
	}
}
