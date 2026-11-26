package main

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{"LIDARR_API_KEY": "k", "ALLOWED_LOGIN": "a@b"}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{LidarrURL: "http://lidarr:8686", LidarrKey: "k", AllowedLogin: "a@b", AuthHeader: "Tailscale-User-Login", TrustedProxies: "127.0.0.1,::1", DataDir: "/data",
		MetadataProfile: "None", ListenAddr: "127.0.0.1:8080", MetadataURL: "https://api.lidarr.audio/api/v0.4",
		ProwlarrURL: "http://prowlarr:9696", ListenBrainzURL: "https://api.listenbrainz.org",
		StockURL: "https://github.com/smile2578/musaarrr/releases/download/stock/stock.json.gz"}
	if c != want {
		t.Fatalf("got %+v\nwant %+v", c, want)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	cases := map[string]map[string]string{
		"PLEX_TOKEN": {"LIDARR_API_KEY": "k", "ALLOWED_LOGIN": "a@b", "PLEX_URL": "http://plex:32400"},
	}
	for name, m := range cases {
		_, err := LoadConfig(env(m))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// Without ALLOWED_LOGIN, people sign in with an account; a proxy header is off until AUTH_HEADER names it.
func TestLoadConfigAccounts(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{"LIDARR_API_KEY": "k"}))
	if err != nil || c.AuthHeader != "" || c.TrustedProxies != "127.0.0.1,::1" {
		t.Fatalf("got %+v, %v", c, err)
	}
	c, _ = LoadConfig(env(map[string]string{"LIDARR_API_KEY": "k", "ALLOWED_LOGIN": "a@b", "AUTH_HEADER": "Remote-User", "TRUSTED_PROXIES": "172.18.0.5"}))
	if c.AuthHeader != "Remote-User" || c.TrustedProxies != "172.18.0.5" {
		t.Fatalf("got %+v", c)
	}
}

// No variable is required: without Lidarr, Musaarr starts and opens on the wizard.
func TestLoadConfigNothingRequired(t *testing.T) {
	c, err := LoadConfig(env(nil))
	if err != nil || c.Ready() == nil {
		t.Fatalf("empty environment: %v, ready %v", err, c.Ready())
	}
	c, _ = LoadConfig(env(map[string]string{"LIDARR_API_KEY": "k", "QUALITY_PROFILE": "Lossless", "ROOT_FOLDER": "/music"}))
	if c.Ready() != nil {
		t.Fatal(c.Ready())
	}
}
