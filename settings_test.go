package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readSettingsFile(t *testing.T, dir string) settingsFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f settingsFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// An install from before the settings file keeps every value it ran with, defaults included.
func TestSettingsMigrateTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	environ := []string{"LIDARR_API_KEY=k", "PLEX_URL=http://plex:32400", "PLEX_TOKEN=p", "PATH=/bin", "TS_AUTHKEY=x", "LISTEN_ADDR=:9"}
	s, err := OpenSettings(dir, environ)
	if err != nil {
		t.Fatal(err)
	}
	f := readSettingsFile(t, dir)
	want := map[string]string{"LIDARR_API_KEY": "k", "PLEX_URL": "http://plex:32400", "PLEX_TOKEN": "p",
		"QUALITY_PROFILE": "Hi-Fi", "ROOT_FOLDER": "/data/music"}
	if f.Version != 1 {
		t.Errorf("version %d", f.Version)
	}
	for k, v := range want {
		if f.Values[k] != v {
			t.Errorf("%s = %q, want %q", k, f.Values[k], v)
		}
	}
	for _, k := range []string{"PATH", "TS_AUTHKEY", "LISTEN_ADDR"} {
		if _, ok := f.Values[k]; ok {
			t.Errorf("%s copied into settings.json", k)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dir, "settings.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if !s.Locked("PLEX_TOKEN") || s.Locked("PROWLARR_URL") {
		t.Error("locked keys")
	}
}

// A fresh install writes nothing until the wizard saves, and has no profile to guess.
func TestSettingsFreshInstall(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSettings(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("settings.json written: %v", err)
	}
	c, err := LoadConfig(s.Getenv)
	if err != nil || c.Ready() == nil {
		t.Fatalf("fresh install ready: %v %v", err, c.Ready())
	}
}

func TestSettingsEnvironmentWins(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenSettings(dir, []string{"LIDARR_API_KEY=old"}); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSettings(dir, []string{"LIDARR_API_KEY=new"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Getenv("LIDARR_API_KEY") != "new" || s.Getenv("QUALITY_PROFILE") != "Hi-Fi" {
		t.Fatalf("got %q %q", s.Getenv("LIDARR_API_KEY"), s.Getenv("QUALITY_PROFILE"))
	}
	if err := s.Save(map[string]string{"LIDARR_API_KEY": "ui"}); err == nil || !strings.Contains(err.Error(), "LIDARR_API_KEY") {
		t.Fatalf("saving a key the environment fixes: %v", err)
	}
}

func TestSettingsSave(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSettings(dir, nil)
	if err := s.Save(map[string]string{"LIDARR_URL": "http://l:8686", "LIDARR_API_KEY": "k", "QUALITY_PROFILE": "Lossless", "ROOT_FOLDER": "/music"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(map[string]string{"PLEX_URL": "http://p:32400", "PLEX_TOKEN": "t", "LIDARR_URL": ""}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(map[string]string{"DATA_DIR": "/etc"}); err == nil {
		t.Fatal("saved a key that is not a setting")
	}
	f := readSettingsFile(t, dir)
	if f.Values["LIDARR_API_KEY"] != "k" || f.Values["PLEX_TOKEN"] != "t" || f.Values["DATA_DIR"] != "" {
		t.Fatalf("file %+v", f.Values)
	}
	if _, kept := f.Values["LIDARR_URL"]; kept {
		t.Error("an emptied value stays in the file")
	}
	s2, _ := OpenSettings(dir, nil)
	c, err := LoadConfig(s2.Getenv)
	if err != nil || c.Ready() != nil || c.LidarrURL != "http://lidarr:8686" || c.QualityProfile != "Lossless" {
		t.Fatalf("reload: %+v %v %v", c, err, c.Ready())
	}
}

func TestSettingsNewerFileRefused(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"version":2,"values":{}}`), 0o600)
	if _, err := OpenSettings(dir, nil); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("got %v", err)
	}
}

// Only page settings come from the file: DATA_DIR or the proxy trust stay environment only.
func TestSettingsFileHoldsOnlySettings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"version":1,"values":{"TRUSTED_PROXIES":"0.0.0.0/0","LIDARR_URL":"http://l"}}`), 0o600)
	s, err := OpenSettings(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Getenv("TRUSTED_PROXIES") != "" || s.Getenv("LIDARR_URL") != "http://l" {
		t.Fatalf("%q %q", s.Getenv("TRUSTED_PROXIES"), s.Getenv("LIDARR_URL"))
	}
}
