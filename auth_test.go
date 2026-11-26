package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// authApp is the security gate around a stub app, with a fresh Auth in its own folder.
func authApp(t *testing.T, header, proxies, adminLogin string) (*Auth, http.Handler) {
	t.Helper()
	a, err := OpenAuth(t.TempDir(), header, proxies, adminLogin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	return a, withSecurity(a, mux)
}

// post sends JSON from addr, with the cookie if one is given.
func post(h http.Handler, path, body, addr string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = addr
	if c != nil {
		r.AddCookie(c)
	}
	return do(h, r)
}

func get(h http.Handler, path string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if c != nil {
		r.AddCookie(c)
	}
	return do(h, r)
}

func sessionOf(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatalf("no session cookie in %d %s", w.Code, w.Body)
	return nil
}

const lan = "192.168.1.20:5000"

func TestPasswordHash(t *testing.T) {
	h := hashPassword("correct horse")
	if !strings.HasPrefix(h, "pbkdf2-sha256$600000$") || strings.Contains(h, "correct") {
		t.Fatalf("hash %q", h)
	}
	if !checkPassword(h, "correct horse") || checkPassword(h, "correct horsE") || checkPassword("garbage", "x") {
		t.Fatal("check")
	}
	if hashPassword("correct horse") == h {
		t.Fatal("same salt twice")
	}
}

func TestFirstRunCreatesTheAdmin(t *testing.T) {
	a, h := authApp(t, "", "", "")
	if w := get(h, "/api/auth", nil); !strings.Contains(w.Body.String(), `"setup":true`) || !strings.Contains(w.Body.String(), `"user":null`) {
		t.Fatalf("state: %s", w.Body)
	}
	if w := get(h, "/api/requests", nil); w.Code != 401 || errCode(w) != "auth.required" {
		t.Fatalf("api before setup: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/auth/setup", `{"name":"alice","password":"short"}`, lan, nil); w.Code != 400 || errCode(w) != "auth.passwordTooShort" {
		t.Fatalf("short password: %d %s", w.Code, w.Body)
	}
	w := post(h, "/api/auth/setup", `{"name":" alice ","password":"long enough"}`, lan, nil)
	if w.Code != 201 {
		t.Fatalf("setup: %d %s", w.Code, w.Body)
	}
	c := sessionOf(t, w)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.Path != "/" {
		t.Errorf("cookie %+v", c)
	}
	if w := get(h, "/api/requests", c); w.Code != 200 {
		t.Fatalf("api with the session: %d", w.Code)
	}
	if w := get(h, "/api/auth", c); !strings.Contains(w.Body.String(), `"name":"alice","admin":true`) || !strings.Contains(w.Body.String(), `"setup":false`) {
		t.Fatalf("state after: %s", w.Body)
	}
	if w := post(h, "/api/auth/setup", `{"name":"eve","password":"long enough"}`, lan, nil); w.Code != 409 {
		t.Fatalf("second setup: %d", w.Code)
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(a.path), "auth.json"))
	if strings.Contains(string(b), "long enough") || strings.Contains(string(b), c.Value) {
		t.Fatalf("auth.json keeps a secret: %s", b)
	}
	if fi, _ := os.Stat(a.path); fi.Mode().Perm() != 0o600 {
		t.Errorf("auth.json mode %v", fi.Mode())
	}
}

// A port published to the Internet must not let a stranger claim a fresh install.
func TestFirstRunOnlyFromTheLocalNetwork(t *testing.T) {
	_, h := authApp(t, "", "", "")
	for _, addr := range []string{"203.0.113.9:4000", "[2001:db8::1]:4000"} {
		if w := post(h, "/api/auth/setup", `{"name":"eve","password":"long enough"}`, addr, nil); w.Code != 403 || errCode(w) != "auth.setupLocalOnly" {
			t.Fatalf("%s: %d %s", addr, w.Code, w.Body)
		}
	}
	for _, addr := range []string{"127.0.0.1:1", "10.0.0.2:1", "100.101.102.103:1", "[fd7a:115c:a1e0::1]:1", "[::1]:1"} {
		if !isLocal(peerAddr(addr)) {
			t.Errorf("%s not local", addr)
		}
	}
	if w := get(h, "/api/auth", nil); !strings.Contains(w.Body.String(), `"setup":true`) {
		t.Fatal("a refused setup created an account")
	}
}

func TestLoginAndLogout(t *testing.T) {
	_, h := authApp(t, "", "", "")
	post(h, "/api/auth/setup", `{"name":"Alice","password":"long enough"}`, lan, nil)
	wrong := post(h, "/api/auth/login", `{"name":"alice","password":"not it"}`, lan, nil)
	nobody := post(h, "/api/auth/login", `{"name":"nobody","password":"long enough"}`, lan, nil)
	if wrong.Code != 401 || nobody.Code != 401 || wrong.Body.String() != nobody.Body.String() || errCode(wrong) != "auth.badCredentials" {
		t.Fatalf("a wrong password and an unknown name must read the same: %s / %s", wrong.Body, nobody.Body)
	}
	w := post(h, "/api/auth/login", `{"name":"ALICE","password":"long enough"}`, lan, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	c := sessionOf(t, w)
	if w := get(h, "/api/requests", c); w.Code != 200 {
		t.Fatalf("with the session: %d", w.Code)
	}
	w = post(h, "/api/auth/logout", "", lan, c)
	if w.Code != 204 || sessionOf(t, w).MaxAge >= 0 {
		t.Fatalf("logout: %d %v", w.Code, w.Result().Cookies())
	}
	if w := get(h, "/api/requests", c); w.Code != 401 {
		t.Fatalf("the old cookie still opens: %d", w.Code)
	}
}

func TestLoginRateLimitedByAddress(t *testing.T) {
	_, h := authApp(t, "", "", "")
	post(h, "/api/auth/setup", `{"name":"alice","password":"long enough"}`, lan, nil)
	for range maxFails {
		post(h, "/api/auth/login", `{"name":"alice","password":"guess"}`, "198.51.100.7:1", nil)
	}
	if w := post(h, "/api/auth/login", `{"name":"alice","password":"long enough"}`, "198.51.100.7:2", nil); w.Code != 429 || errCode(w) != "auth.tooManyAttempts" {
		t.Fatalf("after %d failures: %d %s", maxFails, w.Code, w.Body)
	}
	if w := post(h, "/api/auth/login", `{"name":"alice","password":"long enough"}`, lan, nil); w.Code != 200 {
		t.Fatalf("another address is not blocked: %d", w.Code)
	}
}

func TestSessionSurvivesARestartAndExpires(t *testing.T) {
	a, h := authApp(t, "", "", "")
	c := sessionOf(t, post(h, "/api/auth/setup", `{"name":"alice","password":"long enough"}`, lan, nil))
	b, err := OpenAuth(filepath.Dir(a.path), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h2 := withSecurity(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if w := get(h2, "/api/requests", c); w.Code != 200 {
		t.Fatalf("after a restart: %d", w.Code)
	}
	b.now = func() time.Time { return time.Now().Add(20 * 24 * time.Hour) }
	if w := get(h2, "/api/requests", c); w.Code != 200 { // in use: thirty more days
		t.Fatalf("day 20: %d", w.Code)
	}
	b.now = func() time.Time { return time.Now().Add(45 * 24 * time.Hour) }
	if w := get(h2, "/api/requests", c); w.Code != 200 {
		t.Fatalf("day 45, used on day 20: %d", w.Code)
	}
	b.now = func() time.Time { return time.Now().Add(80 * 24 * time.Hour) }
	if w := get(h2, "/api/requests", c); w.Code != 401 {
		t.Fatalf("day 80, unused for 35 days: %d", w.Code)
	}
}

func TestTrustedHeaderOnlyFromAListedProxy(t *testing.T) {
	_, h := authApp(t, "Remote-User", "172.18.0.5, 10.1.0.0/16", "alice@example.com")
	send := func(addr, user string) int {
		r := httptest.NewRequest("GET", "/api/requests", nil)
		r.RemoteAddr = addr
		r.Header.Set("Remote-User", user)
		return do(h, r).Code
	}
	if code := send("172.18.0.5:80", "alice@example.com"); code != 200 {
		t.Fatalf("listed proxy: %d", code)
	}
	if code := send("10.1.4.4:80", "ALICE@example.com"); code != 200 {
		t.Fatalf("listed range: %d", code)
	}
	if code := send("172.18.0.6:80", "alice@example.com"); code != 401 {
		t.Fatalf("a neighbour forging the header: %d", code)
	}
	if code := send("172.18.0.5:80", "eve"); code != 401 { // a header never creates an account
		t.Fatalf("unknown person: %d", code)
	}
	// The proxy's user names are not Musaarr's: its "alice" may be someone else.
	if code := send("172.18.0.5:80", "alice"); code != 401 {
		t.Fatalf("matched by account name: %d", code)
	}
	if w := get(h, "/api/auth", nil); !strings.Contains(w.Body.String(), `"user":null`) {
		t.Fatal(w.Body.String())
	}
}

// A proxy Musaarr does not know hides who is behind it: no first account through it.
func TestFirstRunNotThroughAnUnknownProxy(t *testing.T) {
	_, h := authApp(t, "", "127.0.0.1, 172.18.0.5", "")
	setup := func(addr string, headers ...string) int {
		r := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"name":"eve","password":"long enough"}`))
		r.Header.Set("Content-Type", "application/json")
		r.RemoteAddr = addr
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Add(headers[i], headers[i+1])
		}
		return do(h, r).Code
	}
	cases := []struct {
		addr    string
		headers []string
	}{
		{"172.18.0.9:1", []string{"X-Forwarded-For", "203.0.113.5"}}, // an unlisted proxy
		{"172.18.0.9:1", []string{"X-Real-IP", "203.0.113.5"}},
		{"172.18.0.9:1", []string{"Forwarded", "for=203.0.113.5"}},
		{"172.18.0.5:1", nil}, // a listed proxy that names nobody
		{"172.18.0.5:1", []string{"X-Forwarded-For", "203.0.113.5"}},
		{"127.0.0.1:1", []string{"X-Forwarded-For", "192.168.1.4", "X-Forwarded-For", "203.0.113.5"}}, // the last hop counts
	}
	for _, c := range cases {
		if code := setup(c.addr, c.headers...); code != 403 {
			t.Errorf("%s %v: %d", c.addr, c.headers, code)
		}
	}
	if code := setup("127.0.0.1:1"); code != 201 { // a browser on the machine itself
		t.Fatalf("loopback: %d", code)
	}
}

// Wrong passwords sent all at once count as they arrive, not as they finish.
func TestLoginRateLimitHoldsUnderABurst(t *testing.T) {
	_, h := authApp(t, "", "", "")
	post(h, "/api/auth/setup", `{"name":"alice","password":"long enough"}`, lan, nil)
	codes := make(chan int, 3*maxFails)
	for range cap(codes) {
		go func() {
			codes <- post(h, "/api/auth/login", `{"name":"alice","password":"guess"}`, "198.51.100.7:1", nil).Code
		}()
	}
	tried := 0
	for range cap(codes) {
		if <-codes == 401 {
			tried++
		}
	}
	if tried > maxFails {
		t.Fatalf("%d passwords tried, %d allowed", tried, maxFails)
	}
}

// Musaarr's own install keeps working unchanged: ALLOWED_LOGIN names its admin, behind Tailscale serve.
func TestAllowedLoginStaysTheAdmin(t *testing.T) {
	a, h := authApp(t, "Tailscale-User-Login", "127.0.0.1", "me@example.com")
	r := httptest.NewRequest("GET", "/api/auth", nil)
	r.RemoteAddr = "127.0.0.1:41000"
	r.Header.Set("Tailscale-User-Login", "Me@Example.com")
	w := do(h, r)
	if !strings.Contains(w.Body.String(), `"name":"me","admin":true,"proxy":true`) || !strings.Contains(w.Body.String(), `"setup":false`) {
		t.Fatalf("state: %s", w.Body)
	}
	if w := post(h, "/api/auth/setup", `{"name":"eve","password":"long enough"}`, "127.0.0.1:1", nil); w.Code != 409 {
		t.Fatalf("setup with an admin from ALLOWED_LOGIN: %d", w.Code)
	}
	if w := post(h, "/api/auth/login", `{"name":"me","password":""}`, lan, nil); w.Code != 401 {
		t.Fatalf("an account without a password opens with none: %d", w.Code)
	}
	b, err := OpenAuth(filepath.Dir(a.path), "Tailscale-User-Login", "127.0.0.1", "me@example.com")
	if err != nil || len(b.users) != 1 {
		t.Fatalf("restart: %v, %d users", err, len(b.users))
	}
}

func TestUnauthenticatedPages(t *testing.T) {
	_, h := authApp(t, "", "", "")
	if w := get(h, "/", nil); w.Code != 303 || w.Header().Get("Location") != "/login.html" {
		t.Fatalf("/: %d %v", w.Code, w.Header())
	}
	if w := get(h, "/login.html", nil); w.Code != 200 {
		t.Fatalf("login page: %d", w.Code)
	}
	if w := get(h, "/healthz", nil); w.Code != 200 {
		t.Fatalf("healthz: %d", w.Code)
	}
}

func TestSecureCookieBehindAnHTTPSProxy(t *testing.T) {
	_, h := authApp(t, "", "127.0.0.1", "")
	r := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"name":"alice","password":"long enough"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "192.168.1.30")
	r.RemoteAddr = "127.0.0.1:9000"
	if c := sessionOf(t, do(h, r)); !c.Secure {
		t.Fatal("not Secure behind an HTTPS proxy")
	}
	r = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"name":"alice","password":"long enough"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-Proto", "https") // from a browser on plain HTTP: a Secure cookie would never come back
	r.RemoteAddr = lan
	if c := sessionOf(t, do(h, r)); c.Secure {
		t.Fatal("Secure because an untrusted client said https")
	}
}

func TestBadProxyList(t *testing.T) {
	if _, err := OpenAuth(t.TempDir(), "Remote-User", "10.0.0.0/8, nas", ""); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXIES") {
		t.Fatalf("got %v", err)
	}
}
