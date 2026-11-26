package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestWizardServer serves a fresh Musaarr with a fake Lidarr, a fake Plex and a fake plex.tv,
// for the browser walk in test/e2e/wizard.test.mjs, which starts it. On its own it skips.
func TestWizardServer(t *testing.T) {
	addr := os.Getenv("MUSAARR_E2E_ADDR")
	if addr == "" {
		t.Skip("started by test/e2e/wizard.test.mjs")
	}
	plex := newFake(t)
	plex.reply("GET /identity", 200, `{"MediaContainer":{"machineIdentifier":"abc"}}`)
	tv, signIn := fakePlexTV(t, plex.srv.URL)
	lidarr := fakeLidarr(t)
	core, h := coreApp(t, nil)
	core.plexTV = tv.URL

	stop := make(chan struct{})
	ctl := http.NewServeMux() // what the browser script does outside Musaarr
	ctl.HandleFunc("POST /approve", func(w http.ResponseWriter, r *http.Request) { signIn() })
	ctl.HandleFunc("POST /stop", func(w http.ResponseWriter, r *http.Request) { close(stop) })
	ctlSrv := httptest.NewServer(ctl)
	defer ctlSrv.Close()

	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go srv.ListenAndServe()
	defer srv.Close()
	fmt.Printf("e2e lidarr=%s plex=%s control=%s\n", lidarr.srv.URL, plex.srv.URL, ctlSrv.URL)
	select {
	case <-stop:
	case <-time.After(5 * time.Minute):
		t.Fatal("the browser script never said stop")
	}
}
