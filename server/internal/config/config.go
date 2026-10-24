// Package config resolves process-level settings from flags and environment.
// Anything an admin should change at runtime lives in the settings table instead.
package config

import (
	"flag"
	"os"
	"path/filepath"
)

type Config struct {
	Addr    string
	DataDir string
	FFmpeg  string
	FFprobe string
	// TraktAPI is Trakt's API address; only changed for testing against a fake.
	TraktAPI string
	// TPDbURL is ThePosterDB's address; only changed for testing against a fake.
	TPDbURL string
	// UpdateRepo is the GitHub repository whose releases carry app updates.
	UpdateRepo string
}

func Load() Config {
	var c Config
	flag.StringVar(&c.Addr, "addr", env("BB_ADDR", ":8484"), "HTTP listen address")
	flag.StringVar(&c.DataDir, "data", env("BB_DATA_DIR", defaultDataDir()), "directory for the database, caches and transcodes")
	flag.StringVar(&c.FFmpeg, "ffmpeg", env("BB_FFMPEG", "ffmpeg"), "path to the ffmpeg binary")
	flag.StringVar(&c.FFprobe, "ffprobe", env("BB_FFPROBE", "ffprobe"), "path to the ffprobe binary")
	flag.StringVar(&c.TPDbURL, "tpdb-url", env("BB_TPDB_URL", "https://theposterdb.com"), "ThePosterDB address (for testing)")
	flag.StringVar(&c.TraktAPI, "trakt-api", env("BB_TRAKT_API", "https://api.trakt.tv"), "Trakt API address (for testing)")
	flag.StringVar(&c.UpdateRepo, "update-repo", env("BB_UPDATE_REPO", "Spacegoose02/BingeBandit"), "GitHub owner/repo that publishes app updates")
	flag.Parse()
	return c
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "BingeBandit")
	}
	return "data"
}
