// Command cinemask-upgrades is the Cinemask Upgrades plugin. It looks over
// the library for movies and episodes below the quality the admin wants,
// using what BingeBandit measured from the files themselves, and has
// Cinemask search its indexers for better copies.
package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"bingebandit/pkg/bbplugin"
)

var manifest = bbplugin.Manifest{
	ID:          "cinemask-upgrades",
	Name:        "Cinemask Upgrades",
	Version:     "1.0.0",
	Description: "Finds movies and episodes below the quality you want and searches Cinemask for better copies.",
	Author:      "BingeBandit",
	Homepage:    "https://github.com/Spacegoose02/cinemask",
	Settings: []bbplugin.Setting{
		{Key: "url", Label: "Cinemask address", Type: "text", Help: "For example http://192.168.1.10:7979"},
		{Key: "api_key", Label: "Cinemask API key", Type: "password",
			Help: "Make one in Cinemask under Account → API keys, signed in as a manager or admin, with full access (not read-only)."},
		{Key: "target", Label: "Lowest resolution you want", Type: "select", Options: []string{"720p", "1080p", "2160p"}, Default: "1080p"},
		{Key: "old_codecs", Label: "Also list old codecs (MPEG-2, DivX/Xvid, VC-1, WMV)", Type: "bool", Default: "true"},
		{Key: "min_bitrate", Label: "Also list files below this bitrate (Mbps; 0 is off)", Type: "number", Default: "0"},
		{Key: "want_hdr", Label: "Also list 4K files without HDR", Type: "bool", Default: "false"},
	},
	Pages: []bbplugin.Page{{Title: "Upgrades", Path: "/", Admin: true}},
}

type app struct {
	p *bbplugin.Plugin
	// files lists the library's files (tests replace it).
	files func(ctx context.Context) ([]file, error)

	mu       sync.Mutex
	settings map[string]string
	last     *report
	lastAt   time.Time
	bulk     bulkState
}

func main() {
	a := &app{}
	a.p = &bbplugin.Plugin{Manifest: manifest, OnEvent: a.onEvent}
	a.p.Handler = a.routes()
	a.files = a.libraryFiles
	log.SetFlags(0)
	if err := a.p.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) onEvent(_ context.Context, e bbplugin.Event) {
	if e.Event != "plugin.settings" {
		return
	}
	var s map[string]string
	if json.Unmarshal(e.Data, &s) == nil {
		a.mu.Lock()
		a.settings = s
		a.last = nil // the rules may have changed
		a.mu.Unlock()
	}
}

// conf returns the settings, fetching them the first time.
func (a *app) conf(ctx context.Context) map[string]string {
	a.mu.Lock()
	s := a.settings
	a.mu.Unlock()
	if s != nil {
		return s
	}
	s, err := a.p.Settings(ctx)
	if err != nil {
		s = map[string]string{}
		for _, st := range manifest.Settings {
			s[st.Key] = st.Default
		}
		return s
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	return s
}

func (a *app) cinemask(ctx context.Context) (*Cinemask, error) {
	s := a.conf(ctx)
	return newCinemask(s["url"], s["api_key"])
}

// libraryFiles pages through the plugin API's /files.
func (a *app) libraryFiles(ctx context.Context) ([]file, error) {
	var all []file
	for offset := 0; ; {
		var page struct {
			Files []file `json:"files"`
			Total int    `json:"total"`
		}
		if err := a.p.Call(ctx, "GET", "/files?limit=1000&offset="+itoa(offset), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Files...)
		offset += len(page.Files)
		if len(page.Files) == 0 || offset >= page.Total {
			return all, nil
		}
	}
}

// scan returns the upgrade report, reusing one from the last ten minutes.
func (a *app) scan(ctx context.Context, fresh bool) (*report, time.Time, error) {
	a.mu.Lock()
	last, at := a.last, a.lastAt
	a.mu.Unlock()
	if last != nil && !fresh && time.Since(at) < 10*time.Minute {
		return last, at, nil
	}
	files, err := a.files(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	rep := find(files, rulesFrom(a.conf(ctx)))
	now := time.Now()
	a.mu.Lock()
	a.last, a.lastAt = &rep, now
	a.mu.Unlock()
	return &rep, now, nil
}
