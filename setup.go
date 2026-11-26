package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The settings API behind the wizard and the settings page, for admins only. Keys and tokens
// go in and never come back out: the page learns only whether one is set.

type settingValue struct {
	Value  string `json:"value,omitempty"`
	Set    bool   `json:"set,omitempty"`    // a secret: whether there is one
	Locked bool   `json:"locked,omitempty"` // fixed by an environment variable
}

type settingsView struct {
	Values map[string]settingValue `json:"values"`
	Ready  bool                    `json:"ready"` // Lidarr is set: the app runs
}

// LidarrChoices is what the wizard offers once Lidarr answers.
type LidarrChoices struct {
	Version          string       `json:"version"`
	QualityProfiles  []string     `json:"qualityProfiles"`
	MetadataProfiles []string     `json:"metadataProfiles"`
	RootFolders      []RootChoice `json:"rootFolders"`
}

// RootChoice is a Lidarr root folder with the profiles Lidarr gives new artists there.
type RootChoice struct {
	Path            string `json:"path"`
	QualityProfile  string `json:"qualityProfile,omitempty"`
	MetadataProfile string `json:"metadataProfile,omitempty"`
}

// setupErr is a check of what the admin typed that failed, with the code the page words.
type setupErr struct {
	code   string
	params map[string]any
}

func badSetup(code string, params map[string]any) *setupErr { return &setupErr{code, params} }

func (e *setupErr) Error() string { return english("error."+e.code, e.params) }

func (e *setupErr) write(w http.ResponseWriter) { fail(w, http.StatusBadRequest, e.code, e.params) }

func secret(k string) bool {
	return strings.HasSuffix(k, "_KEY") || strings.HasSuffix(k, "_TOKEN") || k == "PLEX_CLIENT_ID"
}

func (c *Core) settingsRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/settings", c.getSettings)
	mux.HandleFunc("PUT /api/settings", c.putSettings)
	mux.HandleFunc("POST /api/settings/lidarr/test", c.testLidarr)
	mux.HandleFunc("POST /api/settings/plex/pin", c.newPlexPin)
	mux.HandleFunc("GET /api/settings/plex/pin/{id}", c.plexPinState)
	mux.HandleFunc("POST /api/settings/plex/server", c.choosePlexServer)
	mux.HandleFunc("GET /api/settings/backup", c.backup)
	mux.HandleFunc("POST /api/settings/restore", c.restore)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, ok := c.auth.User(r); !ok || !u.Admin {
			fail(w, http.StatusForbidden, "auth.adminOnly", nil)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (c *Core) view() settingsView {
	v := settingsView{Values: map[string]settingValue{}, Ready: c.cur.Load().app != nil}
	for _, k := range settingKeys {
		val := cmp.Or(c.settings.Getenv(k), configDefaults[k])
		sv := settingValue{Locked: c.settings.Locked(k)}
		if secret(k) {
			sv.Set = val != ""
		} else {
			sv.Value = val
		}
		v.Values[k] = sv
	}
	return v
}

func (c *Core) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, c.view())
}

// putSettings checks the changes against Lidarr before keeping any of them, then restarts the app.
func (c *Core) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Values map[string]string `json:"values"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "setup.badFormat", nil)
		return
	}
	if c.refused(w, in.Values) {
		return
	}
	cfg, err := LoadConfig(func(k string) string {
		if v, ok := in.Values[k]; ok {
			return strings.TrimSpace(v)
		}
		return c.settings.Getenv(k)
	})
	if err != nil {
		fail(w, http.StatusBadRequest, "setup.plexTokenMissing", nil)
		return
	}
	if touchesLidarr(in.Values) && cfg.Ready() == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := checkChoice(ctx, cfg); err != nil {
			err.write(w)
			return
		}
	}
	if err := c.settings.Save(in.Values); err != nil {
		fail(w, http.StatusInternalServerError, "setup.notSaved", nil)
		return
	}
	if err := c.Apply(); err != nil {
		fail(w, http.StatusInternalServerError, "setup.notApplied", nil)
		return
	}
	writeJSON(w, http.StatusOK, c.view())
}

// refused answers the first change the page may not make, if there is one.
func (c *Core) refused(w http.ResponseWriter, changes map[string]string) bool {
	for k, v := range changes {
		switch {
		case !slices.Contains(settingKeys, k):
			fail(w, http.StatusBadRequest, "setup.unknownSetting", map[string]any{"key": k})
		case c.settings.Locked(k):
			fail(w, http.StatusConflict, "setup.locked", map[string]any{"key": k})
		case strings.HasSuffix(k, "_URL") && !webAddress(v):
			fail(w, http.StatusBadRequest, "setup.badAddress", map[string]any{"key": k})
		default:
			continue
		}
		return true
	}
	return !c.keyStaysHome(w, changes)
}

// webAddress is empty, "off", or an http(s) address with no name or password: the page shows it.
func webAddress(raw string) bool {
	if raw = strings.TrimSpace(raw); raw == "" || raw == "off" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// addressKeys pairs each address with the secret Musaarr sends to it.
var addressKeys = map[string]string{"LIDARR_URL": "LIDARR_API_KEY", "PLEX_URL": "PLEX_TOKEN", "PROWLARR_URL": "PROWLARR_API_KEY", "LISTENBRAINZ_URL": "LISTENBRAINZ_TOKEN"}

// keyStaysHome refuses a new address for a saved secret unless the secret comes with it: an
// admin session that is not the admin's own must not send the saved key to a server of its own.
func (c *Core) keyStaysHome(w http.ResponseWriter, changes map[string]string) bool {
	for addr, key := range addressKeys {
		v, moved := changes[addr]
		_, keyToo := changes[key]
		to := cmp.Or(strings.TrimSpace(v), configDefaults[addr])
		if !moved || keyToo || to == "" || c.settings.Getenv(key) == "" ||
			sameURL(to, cmp.Or(c.settings.Getenv(addr), configDefaults[addr])) {
			continue
		}
		if c.settings.Locked(key) {
			fail(w, http.StatusConflict, "setup.addressLocked", map[string]any{"key": key})
		} else {
			fail(w, http.StatusBadRequest, "setup.keyAgain", map[string]any{"key": key})
		}
		return false
	}
	return true
}

func touchesLidarr(changes map[string]string) bool {
	for k := range changes {
		if strings.HasPrefix(k, "LIDARR_") || k == "QUALITY_PROFILE" || k == "METADATA_PROFILE" || k == "ROOT_FOLDER" {
			return true
		}
	}
	return false
}

// checkChoice makes sure Lidarr has the profiles and root folder the settings name.
func checkChoice(ctx context.Context, cfg Config) *setupErr {
	ch, err := checkLidarr(ctx, cfg.LidarrURL, cfg.LidarrKey)
	if err != nil {
		return err
	}
	missing := ""
	switch {
	case !slices.Contains(ch.QualityProfiles, cfg.QualityProfile):
		missing = cfg.QualityProfile
	case !slices.Contains(ch.MetadataProfiles, cfg.MetadataProfile):
		missing = cfg.MetadataProfile
	case !slices.ContainsFunc(ch.RootFolders, func(r RootChoice) bool { return sameURL(r.Path, cfg.RootFolder) }):
		missing = cfg.RootFolder
	default:
		return nil
	}
	return badSetup("setup.lidarrProfiles", map[string]any{"name": missing})
}

// testLidarr answers the wizard's « Tester ». Without a key it uses the saved one, but only
// for the saved address: the page cannot send the key anywhere else.
func (c *Core) testLidarr(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "setup.badFormat", nil)
		return
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" && sameURL(in.URL, cmp.Or(c.settings.Getenv("LIDARR_URL"), configDefaults["LIDARR_URL"])) {
		key = c.settings.Getenv("LIDARR_API_KEY")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ch, err := checkLidarr(ctx, strings.TrimSpace(in.URL), key)
	if err != nil {
		err.write(w)
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func sameURL(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

// checkLidarr asks Lidarr who it is, then lists its profiles and root folders.
func checkLidarr(ctx context.Context, raw, key string) (LidarrChoices, *setupErr) {
	if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return LidarrChoices{}, badSetup("setup.badUrl", nil)
	}
	l := NewLidarr(raw, key)
	var status struct{ AppName, Version string }
	if err := l.do(ctx, "GET", "/api/v1/system/status", nil, nil, &status); err != nil {
		return LidarrChoices{}, lidarrProblem(err)
	}
	if status.AppName != "Lidarr" {
		return LidarrChoices{}, badSetup("setup.notLidarr", nil)
	}
	type named struct {
		ID   int
		Name string
	}
	var qs, ms []named
	var rs []struct {
		Path                     string
		DefaultQualityProfileID  int `json:"defaultQualityProfileId"`
		DefaultMetadataProfileID int `json:"defaultMetadataProfileId"`
	}
	for path, dst := range map[string]any{"/api/v1/qualityprofile": &qs, "/api/v1/metadataprofile": &ms, "/api/v1/rootfolder": &rs} {
		if err := l.do(ctx, "GET", path, nil, nil, dst); err != nil {
			return LidarrChoices{}, lidarrProblem(err)
		}
	}
	ch := LidarrChoices{Version: status.Version, QualityProfiles: []string{}, MetadataProfiles: []string{}, RootFolders: []RootChoice{}}
	qName, mName := map[int]string{}, map[int]string{}
	for _, q := range qs {
		ch.QualityProfiles, qName[q.ID] = append(ch.QualityProfiles, q.Name), q.Name
	}
	for _, m := range ms {
		ch.MetadataProfiles, mName[m.ID] = append(ch.MetadataProfiles, m.Name), m.Name
	}
	for _, r := range rs {
		ch.RootFolders = append(ch.RootFolders, RootChoice{r.Path, qName[r.DefaultQualityProfileID], mName[r.DefaultMetadataProfileID]})
	}
	return ch, nil
}

// lidarrProblem tells the admin what to fix, without Lidarr's own text: it can hold addresses.
func lidarrProblem(err error) *setupErr {
	var le *LidarrError
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	switch {
	case errors.As(err, &le) && le.Status == http.StatusUnauthorized:
		return badSetup("setup.lidarrBadKey", nil)
	case errors.As(err, &le) && le.Status == http.StatusBadRequest && strings.Contains(le.Message, "Invalid Hostname"):
		return badSetup("setup.lidarrHostRejected", nil)
	case errors.As(err, &le) && le.Status == http.StatusNotFound, errors.As(err, &syntax), errors.As(err, &shape):
		return badSetup("setup.notLidarr", nil)
	}
	return badSetup("setup.lidarrUnreachable", nil)
}
