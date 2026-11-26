package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Settings are the values the setup wizard and the settings page write, in settings.json under
// the same names as the environment variables. A variable set in the environment wins, and the
// page shows its field as fixed: automated deployments keep working, and nothing is set twice.

const settingsVersion = 1

// settingKeys are the values the page may change. DATA_DIR, LISTEN_ADDR and the proxy trust
// settings stay environment only: a wrong value there locks the page itself out.
var settingKeys = []string{
	"LIDARR_URL", "LIDARR_API_KEY", "QUALITY_PROFILE", "METADATA_PROFILE", "ROOT_FOLDER",
	"PLEX_URL", "PLEX_TOKEN", "PLEX_CLIENT_ID", "PROWLARR_URL", "PROWLARR_API_KEY",
	"LISTENBRAINZ_URL", "LISTENBRAINZ_TOKEN", "METADATA_URL", "HIRES_TAGS", "MULTICHANNEL_TAGS",
}

// legacyDefaults are the defaults an install from before settings.json ran with, and that a
// new install no longer guesses: the migration writes them down so that install changes nothing.
var legacyDefaults = map[string]string{"QUALITY_PROFILE": "Hi-Fi", "ROOT_FOLDER": "/data/music"}

type settingsFile struct {
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

type Settings struct {
	path string
	env  map[string]string

	mu     sync.Mutex
	values map[string]string
}

// OpenSettings reads dir/settings.json. Without one, an install that ran on environment
// variables (LIDARR_API_KEY was required) has them copied in, so they can later leave the env file.
func OpenSettings(dir string, environ []string) (*Settings, error) {
	s := &Settings{path: filepath.Join(dir, "settings.json"), env: map[string]string{}, values: map[string]string{}}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && v != "" {
			s.env[k] = v
		}
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if s.env["LIDARR_API_KEY"] == "" {
			return s, nil
		}
		for _, k := range settingKeys {
			if v := s.env[k]; v != "" {
				s.values[k] = v
			} else if v := legacyDefaults[k]; v != "" {
				s.values[k] = v
			}
		}
		return s, s.write()
	}
	if err != nil {
		return nil, err
	}
	var f settingsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s illisible (fichier laissé intact) : %w", s.path, err)
	}
	if f.Version > settingsVersion {
		return nil, fmt.Errorf("%s vient d'une version plus récente de Musaarr (version %d)", s.path, f.Version)
	}
	if f.Values != nil {
		s.values = f.Values
	}
	return s, nil
}

// Getenv is os.Getenv over the environment Musaarr started with, then settings.json for the
// keys the page may set: a hand-edited file cannot widen TRUSTED_PROXIES.
func (s *Settings) Getenv(k string) string {
	if v := s.env[k]; v != "" || !slices.Contains(settingKeys, k) {
		return v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[k]
}

// Locked says the environment fixes k: the page shows it and cannot change it.
func (s *Settings) Locked(k string) bool { return s.env[k] != "" }

// Save applies changes ("" removes a value) and writes the file; nothing changes when one is refused.
func (s *Settings) Save(changes map[string]string) error {
	for k := range changes {
		if !slices.Contains(settingKeys, k) {
			return fmt.Errorf("%s n'est pas un réglage", k)
		}
		if s.Locked(k) {
			return fmt.Errorf("%s est fixé par une variable d'environnement", k)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := maps.Clone(s.values)
	for k, v := range changes {
		if v = strings.TrimSpace(v); v == "" {
			delete(s.values, k)
		} else {
			s.values[k] = v
		}
	}
	if err := s.write(); err != nil {
		s.values = old
		return err
	}
	return nil
}

// replace swaps every value for those of a restored backup; keys the page may not set are dropped.
func (s *Settings) replace(values map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.values
	s.values = map[string]string{}
	for k, v := range values {
		if slices.Contains(settingKeys, k) && v != "" {
			s.values[k] = v
		}
	}
	if err := s.write(); err != nil {
		s.values = old
		return err
	}
	return nil
}

// write saves the values. Callers hold s.mu, except OpenSettings, which nobody else sees yet.
func (s *Settings) write() error {
	b, err := json.MarshalIndent(settingsFile{Version: settingsVersion, Values: s.values}, "", " ")
	if err != nil {
		return err
	}
	return writeFileSync(s.path, b)
}
