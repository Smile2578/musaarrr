package main

import (
	"errors"
	"fmt"
)

type Config struct {
	LidarrURL, LidarrKey, PlexURL, PlexToken, AllowedLogin           string
	AuthHeader, TrustedProxies                                       string
	DataDir, QualityProfile, MetadataProfile, RootFolder, ListenAddr string
	MetadataURL, ProwlarrURL, ProwlarrKey                            string
	ListenBrainzURL, ListenBrainzToken                               string
	StockURL                                                         string // the shop's starting stock, "off" for none
	HiResTags, MultichannelTags                                      string // what some indexers' titles carry
}

// configDefaults are the values used when neither the environment nor settings.json has one.
// QUALITY_PROFILE and ROOT_FOLDER have none: the wizard picks them from what Lidarr has.
var configDefaults = map[string]string{
	"LIDARR_URL":       "http://lidarr:8686",
	"TRUSTED_PROXIES":  "127.0.0.1,::1",
	"DATA_DIR":         "/data",
	"METADATA_PROFILE": "None", // Lidarr's built-in: only the requested album
	"LISTEN_ADDR":      "127.0.0.1:8080",
	"METADATA_URL":     "https://api.lidarr.audio/api/v0.4",
	"PROWLARR_URL":     "http://prowlarr:9696",
	"LISTENBRAINZ_URL": "https://api.listenbrainz.org",
	"STOCK_URL":        "https://github.com/smile2578/musaarrr/releases/download/stock/stock.json.gz",
}

func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(k string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return configDefaults[k]
	}
	c := Config{
		LidarrURL:         get("LIDARR_URL"),
		LidarrKey:         get("LIDARR_API_KEY"),
		PlexURL:           get("PLEX_URL"),
		PlexToken:         get("PLEX_TOKEN"),
		AllowedLogin:      get("ALLOWED_LOGIN"), // optional: an admin let in by the proxy header alone
		AuthHeader:        get("AUTH_HEADER"),   // a reverse proxy's identity header, e.g. Remote-User
		TrustedProxies:    get("TRUSTED_PROXIES"),
		DataDir:           get("DATA_DIR"),
		QualityProfile:    get("QUALITY_PROFILE"),
		MetadataProfile:   get("METADATA_PROFILE"),
		RootFolder:        get("ROOT_FOLDER"),
		ListenAddr:        get("LISTEN_ADDR"),
		MetadataURL:       get("METADATA_URL"),
		ProwlarrURL:       get("PROWLARR_URL"),
		ProwlarrKey:       get("PROWLARR_API_KEY"), // optional: without it the Sources tab says so
		ListenBrainzURL:   get("LISTENBRAINZ_URL"),
		HiResTags:         get("HIRES_TAGS"),
		MultichannelTags:  get("MULTICHANNEL_TAGS"),
		ListenBrainzToken: get("LISTENBRAINZ_TOKEN"), // optional: without it, no listens
		StockURL:          get("STOCK_URL"),
	}
	if c.AllowedLogin != "" && c.AuthHeader == "" { // the setup from before accounts: Tailscale serve in front
		c.AuthHeader = "Tailscale-User-Login"
	}
	if c.PlexURL != "" && c.PlexToken == "" {
		return c, fmt.Errorf("PLEX_TOKEN manquant (PLEX_URL est défini)")
	}
	return c, nil
}

// errSetup means Lidarr is not set up yet: Musaarr opens on the setup wizard.
var errSetup = errors.New("Lidarr n'est pas encore réglé")

// Ready says whether Musaarr can talk to Lidarr; until then, only the wizard opens.
func (c Config) Ready() error {
	if c.LidarrKey == "" || c.QualityProfile == "" || c.RootFolder == "" {
		return errSetup
	}
	return nil
}
