package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testAuth trusts the Tailscale header from httptest's default address, with a@b as the admin.
func testAuth(t *testing.T) *Auth {
	t.Helper()
	a, err := OpenAuth(t.TempDir(), "Tailscale-User-Login", "192.0.2.0/24", "a@b")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func gated(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	return withSecurity(testAuth(t), mux)
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGateRejectsWithoutHeader(t *testing.T) {
	w := do(gated(t), httptest.NewRequest("GET", "/api/requests", nil))
	if w.Code != 401 || errCode(w) != "auth.required" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "/api/requests", nil)
	r.Header.Set("Tailscale-User-Login", "A@B")
	if w := do(gated(t), r); w.Code != 200 {
		t.Fatalf("with header: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/requests", nil)
	r.Header.Set("Tailscale-User-Login", "other@b")
	if w := do(gated(t), r); w.Code != 401 {
		t.Fatalf("other login: %d", w.Code)
	}
}

func TestHealthzExempt(t *testing.T) {
	if w := do(gated(t), httptest.NewRequest("GET", "/healthz", nil)); w.Code == 403 {
		t.Fatal("healthz gated")
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := do(gated(t), httptest.NewRequest("GET", "/", nil))
	h := w.Header()
	csp := h.Get("Content-Security-Policy")
	for _, s := range []string{"script-src 'self' 'wasm-unsafe-eval';", "worker-src 'self' blob:;", "img-src 'self' https://imagecache.lidarr.audio https://images.lidarr.audio;"} {
		if !strings.Contains(csp, s) {
			t.Errorf("CSP %q lacks %q", csp, s)
		}
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "same-origin" || h.Get("Strict-Transport-Security") == "" {
		t.Errorf("headers: %v", h)
	}
}

func TestCrossOriginPostRejected(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/requests", strings.NewReader("{}"))
	r.Header.Set("Tailscale-User-Login", "a@b")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	if w := do(gated(t), r); w.Code != 403 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestWriteErrCodeCarriesCodeAndParams(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrCode(rec, http.StatusConflict, "request.alreadySent", "Already sent.", map[string]any{"title": "Kind of Blue"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Error  string         `json:"error"`
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "Already sent." || got.Code != "request.alreadySent" || got.Params["title"] != "Kind of Blue" {
		t.Fatalf("body %s", rec.Body)
	}
	rec = httptest.NewRecorder()
	writeErrCode(rec, http.StatusBadRequest, "search.tooShort", "Too short.", nil)
	if strings.Contains(rec.Body.String(), "params") {
		t.Fatalf("nil params should be left out: %s", rec.Body)
	}
}
