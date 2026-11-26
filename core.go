package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// Core is the server behind the security gate: the setup wizard until Lidarr is set, then the
// app, built again from the settings each time an admin saves them. No restart, no env file.
type Core struct {
	dir      string
	settings *Settings
	auth     *Auth
	store    *Store
	admin    http.Handler // the settings routes, for admins only (setup.go)
	plexTV   string       // plex.tv, where the Plex PIN sign-in happens (plexsetup.go)

	pinMu sync.Mutex
	pins  map[int]*plexPin

	mu  sync.Mutex // one Apply at a time
	cur atomic.Pointer[running]
}

// running is what Core serves now: app is nil in setup mode.
type running struct {
	app    *App
	routes http.Handler
	cancel context.CancelFunc
}

func NewCore(dir string, settings *Settings, auth *Auth, store *Store) *Core {
	c := &Core{dir: dir, settings: settings, auth: auth, store: store, plexTV: "https://plex.tv", pins: map[int]*plexPin{}}
	c.admin = c.settingsRoutes()
	c.cur.Store(&running{cancel: func() {}})
	return c
}

// Apply starts the app the settings describe and stops the one before. Settings that do not
// reach Lidarr yet leave Musaarr on the wizard; an error leaves what runs untouched.
func (c *Core) Apply() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg, err := LoadConfig(c.settings.Getenv)
	if err != nil {
		return err
	}
	setTitleTags(cfg.HiResTags, cfg.MultichannelTags)
	next := &running{cancel: func() {}}
	if cfg.Ready() == nil {
		app, err := c.build(cfg)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(context.Background())
		next = &running{app: app, routes: app.Routes(), cancel: cancel}
		go app.run(ctx, cfg)
	}
	c.cur.Swap(next).cancel()
	return nil
}

// Stop ends the app's background work (refreshes, suggestions, the shop).
func (c *Core) Stop() { c.cur.Load().cancel() }

// build wires the app's clients. The store and data folder outlive it: a new App reads what the
// last one kept. shortcut: the old App's background work stops at its next context check, and
// it keeps its own copy of room.json, so a write it was making (a request, a room change) can
// land once more over the new App's; settings change rarely, so that is left as is.
func (c *Core) build(cfg Config) (*App, error) {
	meta := NewMetadata(cfg.MetadataURL)
	svc := &Service{lidarr: NewLidarr(cfg.LidarrURL, cfg.LidarrKey), meta: meta, store: c.store, root: cfg.RootFolder}
	if cfg.PlexURL != "" {
		svc.plex = NewPlex(cfg.PlexURL, cfg.PlexToken)
	}
	app := NewApp(svc)
	app.meta = meta
	var err error
	if app.room, err = OpenRoom(c.dir); err != nil {
		return nil, err
	}
	app.openShop(c.dir)
	if cfg.StockURL != "off" {
		app.stockURL = cfg.StockURL
	}
	if app.listens = ListensFor(cfg); app.listens == nil && cfg.ListenBrainzURL != "off" {
		log.Print("écoutes coupées : LISTENBRAINZ_TOKEN manquant (jeton dans les réglages de listenbrainz.org)")
	}
	if cfg.ProwlarrKey != "" {
		app.sources = NewSources(NewProwlarr(cfg.ProwlarrURL, cfg.ProwlarrKey))
	}
	return app, nil
}

func (c *Core) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if p == "/api/settings" || strings.HasPrefix(p, "/api/settings/") {
		c.admin.ServeHTTP(w, r)
		return
	}
	if cur := c.cur.Load(); cur.app != nil {
		cur.routes.ServeHTTP(w, r)
		return
	}
	switch {
	case p == "/healthz": // the container is fine, only waiting for its admin
		w.Write([]byte("setup"))
	case strings.HasPrefix(p, "/api/"):
		fail(w, http.StatusServiceUnavailable, "setup.required", nil)
	case p == "/" || p == "/index.html":
		http.Redirect(w, r, "/setup.html", http.StatusSeeOther)
	default:
		webFiles().ServeHTTP(w, r)
	}
}
