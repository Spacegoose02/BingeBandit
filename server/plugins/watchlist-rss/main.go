// Command watchlist-rss is the Watchlist RSS plugin: each person's watchlist
// as an RSS feed, and RSS feeds of films and shows that fill the watchlist.
// It doubles as the reference plugin for docs/PLUGINS.md.
package main

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"sync"
	"time"

	"bingebandit/pkg/bbplugin"
)

var manifest = bbplugin.Manifest{
	ID:          "watchlist-rss",
	Name:        "Watchlist RSS",
	Version:     "1.0.0",
	Description: "Each person's watchlist as an RSS feed, and RSS feeds of movies and shows that add to the watchlist when they're in the library.",
	Author:      "BingeBandit",
	Events:      []string{"playback.finished"},
	Settings: []bbplugin.Setting{
		{Key: "poll_minutes", Label: "Check subscribed feeds every (minutes)", Type: "number", Default: "60"},
		{Key: "remove_watched", Label: "Take movies off the watchlist once they're finished", Type: "bool", Default: "false"},
	},
	Pages: []bbplugin.Page{{Title: "Watchlist feeds", Path: "/"}},
}

type app struct {
	p     *bbplugin.Plugin
	subs  *subStore
	mu    sync.Mutex
	conf  map[string]string
	fetch func(ctx context.Context, url string, allowPrivate bool) ([]byte, error)
}

func main() {
	a := &app{fetch: fetchURL}
	a.p = &bbplugin.Plugin{Manifest: manifest, OnEvent: a.onEvent, Start: a.poll}
	a.p.Handler = a.routes()
	// DataDir is set inside Run, so the store opens lazily on first use.
	a.subs = &subStore{dir: func() string { return a.p.DataDir }}
	log.SetFlags(0)
	if err := a.p.Run(); err != nil {
		log.Fatal(err)
	}
}

// setting reads a setting, fetching them the first time.
func (a *app) setting(ctx context.Context, key string) string {
	a.mu.Lock()
	conf := a.conf
	a.mu.Unlock()
	if conf == nil {
		var err error
		if conf, err = a.p.Settings(ctx); err != nil {
			conf = map[string]string{}
			for _, s := range manifest.Settings {
				conf[s.Key] = s.Default
			}
		} else {
			a.mu.Lock()
			a.conf = conf
			a.mu.Unlock()
		}
	}
	return conf[key]
}

func (a *app) onEvent(ctx context.Context, e bbplugin.Event) {
	switch e.Event {
	case "plugin.settings":
		var conf map[string]string
		if json.Unmarshal(e.Data, &conf) == nil {
			a.mu.Lock()
			a.conf = conf
			a.mu.Unlock()
		}
	case "playback.finished":
		var d struct {
			UserID int64  `json:"userId"`
			ItemID int64  `json:"itemId"`
			Kind   string `json:"kind"`
		}
		if json.Unmarshal(e.Data, &d) != nil || d.Kind != "movie" || d.UserID == 0 {
			return
		}
		if a.setting(ctx, "remove_watched") != "true" {
			return
		}
		path := "/users/" + strconv.FormatInt(d.UserID, 10) + "/watchlist/" + strconv.FormatInt(d.ItemID, 10)
		if err := a.p.Call(ctx, "DELETE", path, nil, nil); err != nil {
			log.Printf("removing finished movie %d from user %d's watchlist: %v", d.ItemID, d.UserID, err)
		}
	}
}

// poll checks each subscribed feed when it's due.
func (a *app) poll(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		every, _ := strconv.Atoi(a.setting(ctx, "poll_minutes"))
		if every < 5 {
			every = 5
		}
		for _, s := range a.subs.due(time.Duration(every) * time.Minute) {
			a.check(ctx, s)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
