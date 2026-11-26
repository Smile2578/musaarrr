package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// wasm-unsafe-eval and blob: workers: three.js decodes the 3D shop's KTX2 textures in WebAssembly, in
// workers it starts from a blob of the vendored transcoder.
const csp = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self' blob:; style-src 'self'; " +
	"img-src 'self' https://imagecache.lidarr.audio https://images.lidarr.audio; " +
	"connect-src 'self'; manifest-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// writeErrCode is writeErr with a code the page translates (web/i18n, key "error."+code);
// msg stays the English fallback for API clients, and params fill the translation's {holes}.
func writeErrCode(w http.ResponseWriter, status int, code, msg string, params map[string]any) {
	writeJSON(w, status, struct {
		Error  string         `json:"error"`
		Code   string         `json:"code"`
		Params map[string]any `json:"params,omitempty"`
	}{msg, code, params})
}

// withSecurity sets security headers, rejects cross-origin writes and lets only signed-in people
// reach the API (see auth.go). The page files are public: they hold nothing of anyone's.
func withSecurity(auth *Auth, next http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusForbidden, "auth.crossSite", nil)
	}))
	gated, signIn := cop.Handler(next), cop.Handler(auth.routes())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		p := r.URL.Path
		if p == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		// CrossOriginProtection lets GET through; another site on the tailnet must not start a search.
		if site := r.Header.Get("Sec-Fetch-Site"); strings.HasPrefix(p, "/api/") && site != "" && site != "same-origin" && site != "none" {
			fail(w, http.StatusForbidden, "auth.crossSite", nil)
			return
		}
		if p == "/api/auth" || strings.HasPrefix(p, "/api/auth/") {
			signIn.ServeHTTP(w, r)
			return
		}
		if _, _, ok := auth.User(r); !ok {
			switch {
			case strings.HasPrefix(p, "/api/"):
				fail(w, http.StatusUnauthorized, "auth.required", nil)
				return
			case p == "/" || p == "/index.html":
				http.Redirect(w, r, "/login.html", http.StatusSeeOther)
				return
			}
		}
		gated.ServeHTTP(w, r)
	})
}
