package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "vérifie /healthz et sort (pour Docker)")
	stockOut := flag.String("build-stock", "", "construit le stock du disquaire dans ce fichier (tâche CI mensuelle) et sort")
	mbDump := flag.String("mb", "-", "avec -build-stock : le dump JSON MusicBrainz des release groups (- : entrée standard)")
	discogsDump := flag.String("discogs", "", "avec -build-stock : le dump Discogs des masters (.xml.gz) ; sans lui, les genres de Wikidata")
	stockN := flag.Int("n", 40000, "avec -build-stock : le nombre d'albums visé")
	sources := flag.String("sources", "", "avec -build-stock : les versions des dumps, « musicbrainz=…,discogs=… »")
	flag.Parse()
	if *stockOut != "" {
		if err := runBuildStock(*stockOut, *mbDump, *discogsDump, *stockN, *sources); err != nil {
			log.Fatalf("stock : %v", err)
		}
		return
	}
	// DATA_DIR and LISTEN_ADDR come from the environment only: settings.json lives in DATA_DIR.
	cfg, err := LoadConfig(os.Getenv)
	if *healthcheck {
		probe(cfg, err)
	}
	settings, err := OpenSettings(cfg.DataDir, os.Environ())
	if err != nil {
		log.Fatal(err)
	}
	store, err := OpenStore(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	auth, err := OpenAuth(cfg.DataDir, cfg.AuthHeader, cfg.TrustedProxies, cfg.AllowedLogin)
	if err != nil {
		log.Fatal(err)
	}
	core := NewCore(cfg.DataDir, settings, auth, store)
	if err := core.Apply(); err != nil {
		log.Printf("réglages : %v (l'assistant s'ouvre pour les corriger)", err)
	}
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: withSecurity(auth, core),
		ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Printf("Musaarr écoute sur %s", cfg.ListenAddr)
	log.Fatal(srv.ListenAndServe())
}

// probe exits 0 when the running Musaarr answers /healthz (Docker's HEALTHCHECK has no shell to curl with).
func probe(cfg Config, cfgErr error) {
	addr := "127.0.0.1:8080"
	if cfgErr == nil {
		addr = cfg.ListenAddr
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		os.Exit(1)
	}
	os.Exit(0)
}

// run resolves the Lidarr profiles, then refreshes requests every 10 s (or right after a change)
// and reloads the Plex index every 10 min, or every minute for 30 min after an import.
func (a *App) run(ctx context.Context, cfg Config) {
	s := a.svc
	for {
		p, err := s.lidarr.Profiles(ctx, cfg.QualityProfile, cfg.MetadataProfile, cfg.RootFolder)
		if err == nil {
			s.mu.Lock()
			s.prof = p
			s.mu.Unlock()
			a.setReady("")
			break
		}
		a.setReady(err.Error())
		log.Printf("Lidarr pas prêt : %v (nouvel essai dans 30 s)", err)
		select {
		case <-ctx.Done(): // new settings: another App takes over
			return
		case <-time.After(30 * time.Second):
		}
	}
	if s.plex != nil {
		if err := s.plex.Reload(ctx); err != nil {
			log.Printf("Plex : %v", err)
		}
	}
	go a.suggestDaily(ctx)
	go func() { // each hour, the Japanese fronts of the records a rebuild brought
		for {
			a.warmCovers(ctx)
			wait := time.Hour
			if a.shopNow() == nil {
				wait = time.Minute // the first stock is being built
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-a.nudge:
		}
		s.Refresh(ctx, time.Now())
		if s.plex != nil && (s.plex.Age() > 10*time.Minute || (waitingForPlex(s.store, time.Now()) && s.plex.Age() > time.Minute)) {
			if err := s.plex.Reload(ctx); err != nil {
				log.Printf("Plex : %v", err)
			}
		}
	}
}

func waitingForPlex(st *Store, now time.Time) bool {
	for _, r := range st.All() {
		if recentImport(r, now) || r.Status == "picked" {
			return true
		}
	}
	return false
}
