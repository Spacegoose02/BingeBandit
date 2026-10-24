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
	// UpdateRepo is the GitHub repository whose releases carry app updates.
	UpdateRepo string
	// PluginsDir holds bundled plugins; <data>/plugins is always searched too.
	PluginsDir string
}

func Load() Config {
	var c Config
	flag.StringVar(&c.Addr, "addr", env("BB_ADDR", ":8484"), "HTTP listen address")
	flag.StringVar(&c.DataDir, "data", env("BB_DATA_DIR", defaultDataDir()), "directory for the database, caches and transcodes")
	flag.StringVar(&c.FFmpeg, "ffmpeg", env("BB_FFMPEG", "ffmpeg"), "path to the ffmpeg binary")
	flag.StringVar(&c.FFprobe, "ffprobe", env("BB_FFPROBE", "ffprobe"), "path to the ffprobe binary")
	flag.StringVar(&c.TraktAPI, "trakt-api", env("BB_TRAKT_API", "https://api.trakt.tv"), "Trakt API address (for testing)")
	flag.StringVar(&c.UpdateRepo, "update-repo", env("BB_UPDATE_REPO", "Spacegoose02/BingeBandit"), "GitHub owner/repo that publishes app updates")
	flag.StringVar(&c.PluginsDir, "plugins", env("BB_PLUGINS_DIR", defaultPluginsDir()), "folder of bundled plugins (each in its own folder with a plugin.json)")
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

// defaultPluginsDir is "plugins" next to the executable.
func defaultPluginsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "plugins")
}
