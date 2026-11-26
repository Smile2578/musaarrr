package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Who may open Musaarr. A person signs in with their own name and password, or a reverse proxy
// listed in TRUSTED_PROXIES vouches for them in a header (Tailscale serve, Authelia, authentik).
// Accounts and sessions live in auth.json, next to the requests.

const (
	sessionCookie = "musaarr_session"
	sessionLife   = 30 * 24 * time.Hour // a session unused this long ends; using it extends it
	hashIter      = 600_000             // PBKDF2-SHA256, the OWASP figure: about 0.1 s per try
	maxFails      = 10                  // failed sign-ins per address and window before a pause
	failWindow    = 15 * time.Minute
)

type User struct {
	ID      int       `json:"id"`
	Name    string    `json:"name"`
	Login   string    `json:"login,omitempty"` // the identity a trusted proxy sends for this person
	Hash    string    `json:"hash,omitempty"`  // "": no password, only a trusted proxy lets them in
	Admin   bool      `json:"admin"`
	Created time.Time `json:"created"`
}

type session struct {
	User    int       `json:"user"`
	Expires time.Time `json:"expires"`
}

type authFile struct {
	Users    []User             `json:"users"`
	Sessions map[string]session `json:"sessions"` // by the SHA-256 of the cookie: the file opens no session
}

type Auth struct {
	path       string
	header     string         // the trusted proxy's identity header, "" for none
	adminLogin string         // ALLOWED_LOGIN: the admin that header names, kept through a restore
	proxies    []netip.Prefix // where that header, X-Forwarded-For and X-Forwarded-Proto are believed from
	now        func() time.Time

	mu       sync.Mutex
	users    []User
	sessions map[string]session
	fails    map[netip.Addr][]time.Time
	warned   map[netip.Addr]bool // addresses already logged for sending the header unasked
}

// OpenAuth reads dir/auth.json. adminLogin (ALLOWED_LOGIN) names an admin let in by the proxy header
// only, so an install that predates accounts keeps opening as before.
func OpenAuth(dir, header, proxies, adminLogin string) (*Auth, error) {
	a := &Auth{path: filepath.Join(dir, "auth.json"), header: header, adminLogin: adminLogin, now: time.Now,
		sessions: map[string]session{}, fails: map[netip.Addr][]time.Time{}, warned: map[netip.Addr]bool{}}
	for _, p := range strings.Split(proxies, ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		pre, err := netip.ParsePrefix(p)
		if err != nil {
			ip, ierr := netip.ParseAddr(p)
			if ierr != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES : %q n'est ni une adresse IP ni une plage (ex. 172.18.0.0/16)", p)
			}
			pre = netip.PrefixFrom(ip.Unmap(), ip.Unmap().BitLen())
		}
		a.proxies = append(a.proxies, pre.Masked())
	}
	b, err := os.ReadFile(a.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		var f authFile
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s illisible (fichier laissé intact) : %w", a.path, err)
		}
		a.users = f.Users
		if f.Sessions != nil {
			a.sessions = f.Sessions
		}
	}
	if a.addProxyAdmin() {
		if err := a.save(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// addProxyAdmin gives ALLOWED_LOGIN its admin account when the accounts lack it, and says
// whether it did. Callers hold a.mu, or own a.
func (a *Auth) addProxyAdmin() bool {
	if a.adminLogin == "" || a.byLogin(a.adminLogin) != nil {
		return false
	}
	name, _, _ := strings.Cut(a.adminLogin, "@")
	if name == "" || a.byName(name) != nil {
		name = a.adminLogin
	}
	a.users = append(a.users, User{ID: a.nextID(), Name: name, Login: a.adminLogin, Admin: true, Created: a.now()})
	return true
}

// replace swaps every account and session for those of a restored backup.
func (a *Auth) replace(f authFile) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	users, sessions := a.users, a.sessions
	a.users, a.sessions = f.Users, f.Sessions
	if a.sessions == nil {
		a.sessions = map[string]session{}
	}
	a.addProxyAdmin()
	if err := a.save(); err != nil {
		a.users, a.sessions = users, sessions // the accounts in use stay those on disk
		return err
	}
	return nil
}

// byLogin finds whom a trusted proxy names. Never by account name: the proxy's "alice" may be another person.
func (a *Auth) byLogin(login string) *User {
	for i := range a.users {
		if a.users[i].Login != "" && strings.EqualFold(a.users[i].Login, login) {
			return &a.users[i]
		}
	}
	return nil
}

func (a *Auth) byName(name string) *User {
	for i := range a.users {
		if strings.EqualFold(a.users[i].Name, name) {
			return &a.users[i]
		}
	}
	return nil
}

func (a *Auth) nextID() int {
	id := 0
	for _, u := range a.users {
		id = max(id, u.ID)
	}
	return id + 1
}

// save writes auth.json without the sessions that ended. Callers hold a.mu.
func (a *Auth) save() error {
	now := a.now()
	for k, s := range a.sessions {
		if !now.Before(s.Expires) {
			delete(a.sessions, k)
		}
	}
	b, err := json.MarshalIndent(authFile{Users: a.users, Sessions: a.sessions}, "", " ")
	if err != nil {
		return err
	}
	return writeFileSync(a.path, b)
}

// User says who sent r: the person a trusted proxy names, else the one its session cookie opens.
// proxy is true when the proxy named them: signing out of Musaarr would change nothing.
func (a *Auth) User(r *http.Request) (u User, proxy, ok bool) {
	if a.header != "" {
		if login := r.Header.Get(a.header); login != "" {
			a.mu.Lock()
			if a.trusted(r) {
				if p := a.byLogin(login); p != nil {
					a.mu.Unlock()
					return *p, true, true
				}
			} else if peer := peerAddr(r.RemoteAddr); !a.warned[peer] && len(a.warned) < 100 {
				a.warned[peer] = true
				log.Printf("en-tête %s ignoré : %s n'est pas dans TRUSTED_PROXIES", a.header, peer)
			}
			a.mu.Unlock()
		}
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return User{}, false, false
	}
	key := tokenKey(c.Value)
	a.mu.Lock()
	defer a.mu.Unlock()
	s, found := a.sessions[key]
	now := a.now()
	if !found || !now.Before(s.Expires) {
		return User{}, false, false
	}
	var p *User
	for i := range a.users {
		if a.users[i].ID == s.User {
			p = &a.users[i]
		}
	}
	if p == nil {
		return User{}, false, false
	}
	if s.Expires.Sub(now) < sessionLife-24*time.Hour { // renewed at most once a day, so not one write per request
		s.Expires = now.Add(sessionLife)
		a.sessions[key] = s
		if err := a.save(); err != nil {
			log.Printf("session non prolongée : %v", err) // it still runs until its old end
		}
	}
	return *p, false, true
}

func peerAddr(remoteAddr string) netip.Addr {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	return ap.Addr().Unmap()
}

func (a *Auth) trusted(r *http.Request) bool {
	peer := peerAddr(r.RemoteAddr)
	for _, p := range a.proxies {
		if p.Contains(peer) {
			return true
		}
	}
	return false
}

// client is the address of the person: the one a trusted proxy names, else the peer.
func (a *Auth) client(r *http.Request) netip.Addr {
	if a.trusted(r) {
		if ip, ok := forwardedFor(r); ok {
			return ip
		}
	}
	return peerAddr(r.RemoteAddr)
}

// forwardedFor is the last hop of X-Forwarded-For, the one the nearest proxy wrote. The proxy
// must set or append it: one that passes the client's own copy through lets them pick it.
func forwardedFor(r *http.Request) (netip.Addr, bool) {
	xff := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	ip, err := netip.ParseAddr(strings.TrimSpace(xff[strings.LastIndex(xff, ",")+1:]))
	return ip.Unmap(), err == nil
}

// fromHome says the person is on the local network, as far as Musaarr can tell. Through a listed
// proxy, it is the address the proxy names; through an unknown one, never, since it hides who is behind.
func (a *Auth) fromHome(r *http.Request) bool {
	if a.trusted(r) {
		if ip, ok := forwardedFor(r); ok {
			return isLocal(ip)
		}
		return peerAddr(r.RemoteAddr).IsLoopback() // a browser on the machine itself
	}
	for _, h := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return isLocal(peerAddr(r.RemoteAddr))
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10") // Tailscale's addresses

// isLocal: loopback, a private network or a tailnet, never the Internet.
func isLocal(ip netip.Addr) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || cgnat.Contains(ip)
}

func (a *Auth) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth", a.state)
	mux.HandleFunc("POST /api/auth/setup", a.setup)
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	return mux
}

type publicUser struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	Proxy bool   `json:"proxy"` // signed in by the proxy: no sign-out button
}

func (a *Auth) state(w http.ResponseWriter, r *http.Request) {
	var out struct {
		User  *publicUser `json:"user"`
		Setup bool        `json:"setup"` // no account yet: the first one becomes the admin
	}
	if u, proxy, ok := a.User(r); ok {
		out.User = &publicUser{u.Name, u.Admin, proxy}
	}
	a.mu.Lock()
	out.Setup = len(a.users) == 0
	a.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

type credentials struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func readCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	var in credentials
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "auth.badFormat", nil)
		return in, false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		fail(w, 400, "auth.badFormat", nil)
		return in, false
	}
	in.Name = strings.TrimSpace(in.Name)
	return in, true
}

// setup creates the first account, an admin, and signs it in. Only from the local network:
// a port published to the Internet must not hand a fresh install to the first visitor.
func (a *Auth) setup(w http.ResponseWriter, r *http.Request) {
	if !a.fromHome(r) {
		fail(w, 403, "auth.setupLocalOnly", nil)
		return
	}
	in, ok := readCredentials(w, r)
	if !ok {
		return
	}
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 64 || strings.ContainsFunc(in.Name, unicode.IsControl):
		fail(w, 400, "auth.badName", nil)
		return
	case len(in.Password) < 8:
		fail(w, 400, "auth.passwordTooShort", nil)
		return
	case len(in.Password) > 256:
		fail(w, 400, "auth.passwordTooLong", nil)
		return
	}
	a.mu.Lock()
	done := len(a.users) > 0
	a.mu.Unlock()
	if done { // before hashing: a set-up install spends no CPU on this
		fail(w, 409, "auth.alreadySetUp", nil)
		return
	}
	hash := hashPassword(in.Password) // outside the lock: it takes a tenth of a second
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.users) > 0 { // another setup won meanwhile
		fail(w, 409, "auth.alreadySetUp", nil)
		return
	}
	a.users = []User{{ID: 1, Name: in.Name, Hash: hash, Admin: true, Created: a.now()}}
	token := a.newSession(1)
	if err := a.save(); err != nil {
		a.users, a.sessions = nil, map[string]session{}
		log.Printf("compte non enregistré : %v", err)
		fail(w, 500, "auth.notSaved", nil)
		return
	}
	a.setCookie(w, r, token, sessionLife)
	writeJSON(w, 201, map[string]any{"user": publicUser{in.Name, true, false}})
}

// dummyHash is checked for a name nobody has, so it takes as long as a wrong password.
var dummyHash = sync.OnceValue(func() string { return hashPassword(rand.Text()) })

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	ip := a.client(r)
	if ip.Is6() {
		ip = netip.PrefixFrom(ip, 64).Masked().Addr() // one machine has a whole /64
	}
	// The try counts as failed until the password checks, so a burst sent at once cannot outrun the limit.
	a.mu.Lock()
	recent := a.recentFails(ip)
	blocked := len(recent) >= maxFails
	if !blocked {
		if len(a.fails) > 10_000 { // shortcut: forgets every address at once, fine for a home server
			clear(a.fails)
		}
		a.fails[ip] = append(recent, a.now())
	}
	a.mu.Unlock()
	if blocked {
		fail(w, 429, "auth.tooManyAttempts", nil)
		return
	}
	in, ok := readCredentials(w, r)
	if !ok {
		return
	}
	a.mu.Lock()
	var u User
	if p := a.byName(in.Name); p != nil {
		u = *p
	}
	a.mu.Unlock()
	hash := u.Hash
	if hash == "" {
		hash = dummyHash()
	}
	if !checkPassword(hash, in.Password) || u.Hash == "" {
		fail(w, 401, "auth.badCredentials", nil) // never says which
		return
	}
	a.mu.Lock()
	delete(a.fails, ip)
	token := a.newSession(u.ID)
	err := a.save()
	a.mu.Unlock()
	if err != nil {
		log.Printf("session non enregistrée : %v", err) // it works until Musaarr restarts
	}
	a.setCookie(w, r, token, sessionLife)
	writeJSON(w, 200, map[string]any{"user": publicUser{u.Name, u.Admin, false}})
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, tokenKey(c.Value))
		if err := a.save(); err != nil {
			log.Printf("déconnexion non enregistrée : %v", err)
		}
		a.mu.Unlock()
	}
	a.setCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

// recentFails drops the failures older than the window. Callers hold a.mu.
func (a *Auth) recentFails(ip netip.Addr) []time.Time {
	cut := a.now().Add(-failWindow)
	kept := a.fails[ip][:0]
	for _, t := range a.fails[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(a.fails, ip)
	}
	return kept
}

// newSession returns a fresh cookie value for user id. Callers hold a.mu and save.
func (a *Auth) newSession(id int) string {
	token := rand.Text()
	a.sessions[tokenKey(token)] = session{User: id, Expires: a.now().Add(sessionLife)}
	return token
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// setCookie keeps the cookie for 400 days, the most a browser allows: the session's end is the server's.
func (a *Auth) setCookie(w http.ResponseWriter, r *http.Request, token string, life time.Duration) {
	age := -1
	if life > 0 {
		age = 400 * 24 * 3600
	}
	https := r.TLS != nil || (a.trusted(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: age,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: https})
}

// hashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<key>", base64 without padding.
func hashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := derive(password, salt, hashIter, 32)
	enc := base64.RawStdEncoding
	return "pbkdf2-sha256$" + strconv.Itoa(hashIter) + "$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key)
}

func checkPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	enc := base64.RawStdEncoding
	salt, serr := enc.DecodeString(parts[2])
	want, werr := enc.DecodeString(parts[3])
	if err != nil || serr != nil || werr != nil || iter < 1 || iter > 10_000_000 {
		return false
	}
	got := derive(password, salt, iter, len(want))
	return got != nil && subtle.ConstantTimeCompare(got, want) == 1
}

// hashSlots caps the PBKDF2 runs at once to the CPUs: a flood of sign-ins queues instead of starving the rest.
var hashSlots = make(chan struct{}, runtime.GOMAXPROCS(0))

func derive(password string, salt []byte, iter, n int) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, n)
	if err != nil {
		return nil
	}
	return key
}
