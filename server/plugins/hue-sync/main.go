// Command hue-sync is the Hue Sync plugin: Philips Hue lights follow the
// colours on screen while something plays. It reads the playing file's
// picture a little ahead of the player with ffmpeg, and keeps in step with
// the player through BingeBandit's player.* events.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"sync"

	"bingebandit/pkg/bbplugin"
)

var manifest = bbplugin.Manifest{
	ID:          "hue-sync",
	Name:        "Hue Sync",
	Version:     "1.3.0",
	Description: "Philips Hue lights follow the colours on screen while movies and shows play.",
	Author:      "BingeBandit",
	Events:      []string{"player.play", "player.pause", "player.resume", "player.progress", "player.stop"},
	Settings: []bbplugin.Setting{
		{Key: "users", Label: "Only for these people (comma-separated; empty for everyone)", Type: "text"},
		{Key: "clients", Label: "Only on these devices (comma-separated, e.g. Android TV; empty for all)", Type: "text"},
		{Key: "brightness", Label: "Maximum brightness (%)", Type: "number", Default: "80"},
		{Key: "updates_per_second", Label: "Updates per second (the bridge takes about 10 light changes a second in total)", Type: "number", Default: "4"},
		{Key: "delay_ms", Label: "Delay (ms): raise it if the lights change before the picture", Type: "number", Default: "0"},
		{Key: "theater_brightness", Label: "Theater mode brightness (%)", Type: "number", Default: "8",
			Help: "How far the lights dim for people who pick Theater; 0 turns them off"},
		{Key: "on_pause", Label: "When paused", Type: "select", Options: []string{"keep", "bright"}, Default: "bright",
			Help: "keep: hold the colours; bright: warm white so you can see the room"},
		{Key: "on_stop", Label: "When playback stops", Type: "select", Options: []string{"restore", "off", "leave"}, Default: "restore",
			Help: "restore: put the lights back as they were"},
	},
	Pages: []bbplugin.Page{{Title: "Hue Sync setup", Path: "/", Admin: true}},
	// Nobody's lights move until they pick Sync or Theater for themselves
	// (Account on the web, Settings in the TV app, or the player's options).
}

type app struct {
	p      *bbplugin.Plugin
	conf   *configStore
	engine *Engine
	ffmpeg string
	// fileURL is where ffmpeg reads a file (tests point it at a local file).
	fileURL func(fileID int64) (url, key string)

	mu       sync.Mutex
	settings map[string]string
}

func main() {
	a := &app{ffmpeg: os.Getenv("BINGEBANDIT_FFMPEG")}
	if a.ffmpeg == "" {
		a.ffmpeg = "ffmpeg"
	}
	a.engine = &Engine{a: a, status: "Idle"}
	a.fileURL = func(fileID int64) (string, string) {
		server, key, _ := a.p.Connection()
		return server + "/api/plugin/v1/files/" + strconv.FormatInt(fileID, 10) + "/direct", key
	}
	a.p = &bbplugin.Plugin{Manifest: manifest, OnEvent: a.onEvent}
	a.p.Handler = a.routes()
	a.conf = &configStore{dir: func() string { return a.p.DataDir }}
	log.SetFlags(0)
	if err := a.p.Run(); err != nil {
		log.Fatal(err)
	}
}

func (a *app) onEvent(ctx context.Context, e bbplugin.Event) {
	if e.Event == "plugin.settings" {
		var s map[string]string
		if json.Unmarshal(e.Data, &s) == nil {
			a.mu.Lock()
			a.settings = s
			a.mu.Unlock()
		}
		return
	}
	a.engine.onEvent(ctx, e)
}

// setting reads a setting, fetching them the first time.
func (a *app) setting(ctx context.Context, key string) string {
	a.mu.Lock()
	s := a.settings
	a.mu.Unlock()
	if s == nil {
		var err error
		if s, err = a.p.Settings(ctx); err != nil {
			s = map[string]string{}
			for _, st := range manifest.Settings {
				s[st.Key] = st.Default
			}
		} else {
			a.mu.Lock()
			a.settings = s
			a.mu.Unlock()
		}
	}
	return s[key]
}
