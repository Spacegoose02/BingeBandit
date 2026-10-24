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
}

func Load() Config {
	var c Config
	flag.StringVar(&c.Addr, "addr", env("BB_ADDR", ":8484"), "HTTP listen address")
	flag.StringVar(&c.DataDir, "data", env("BB_DATA_DIR", defaultDataDir()), "directory for the database, caches and transcodes")
	flag.StringVar(&c.FFmpeg, "ffmpeg", env("BB_FFMPEG", "ffmpeg"), "path to the ffmpeg binary")
	flag.StringVar(&c.FFprobe, "ffprobe", env("BB_FFPROBE", "ffprobe"), "path to the ffprobe binary")
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
