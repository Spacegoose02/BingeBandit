// Command game-lights is the Game Lights plugin: Philips Hue lights flash
// in a team's colours when the team people follow scores, and red when it
// turns the ball over. It follows the games BingeBandit's sports tracker
// reports (the teams people pick on the Sports page), reading ESPN's
// play-by-play for football and score changes for other sports.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"bingebandit/pkg/bbplugin"
)

var manifest = bbplugin.Manifest{
	ID:          "game-lights",
	Name:        "Game Lights",
	Version:     "1.0.0",
	Description: "Philips Hue lights flash in your team's colours when it scores, from the teams people follow on the Sports page.",
	Author:      "BingeBandit",
	Events:      []string{"sports.game.start", "sports.game.live", "sports.score", "sports.game.end"},
	Settings: []bbplugin.Setting{
		{Key: "users", Label: "Follow the teams of these people (comma-separated; empty for everyone)", Type: "text"},
		{Key: "delay_seconds", Label: "Delay (seconds): how far your TV runs behind ESPN", Type: "number", Default: "25",
			Help: "Raise it until the lights flash with the picture, not before."},
		{Key: "brightness", Label: "Maximum brightness (%)", Type: "number", Default: "100"},
		{Key: "reduced_flashing", Label: "Reduced flashing", Type: "select", Options: []string{"off", "on"}, Default: "off",
			Help: "on: slower, gentler flashes for anyone sensitive to flashing lights"},
	},
	Pages: []bbplugin.Page{{Title: "Game Lights setup", Path: "/", Admin: true}},
}

type app struct {
	p        *bbplugin.Plugin
	conf     *configStore
	runner   *Runner
	follower *Follower

	mu       sync.Mutex
	settings map[string]string
	log      []logLine
}

type logLine struct {
	At   int64  `json:"at"`
	Text string `json:"text"`
}

func newApp(dir func() string) *app {
	a := &app{}
	a.conf = &configStore{dir: dir}
	a.runner = &Runner{a: a, gap: 100 * time.Millisecond, bridge: func(c Config) bridgeAPI { return newBridge(c.Bridge, c.Username) }}
	a.follower = &Follower{a: a, every: 4 * time.Second}
	return a
}

func main() {
	var p *bbplugin.Plugin
	a := newApp(func() string { return p.DataDir })
	p = &bbplugin.Plugin{Manifest: manifest, OnEvent: a.onEvent, Start: a.sweep}
	a.p = p
	p.Handler = a.routes()
	log.SetFlags(0)
	if err := p.Run(); err != nil {
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
	a.follower.onEvent(ctx, e.Event, e.Data)
}

// sweep drops games that stopped being reported, until shutdown.
func (a *app) sweep(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.runner.Reset()
			return
		case <-t.C:
			a.follower.sweep()
		}
	}
}

// setting reads a setting, fetching them the first time.
func (a *app) setting(ctx context.Context, key string) string {
	a.mu.Lock()
	s := a.settings
	a.mu.Unlock()
	if s == nil {
		var err error
		if a.p == nil {
			err = bbplugin.ErrNotPaired
		} else {
			s, err = a.p.Settings(ctx)
		}
		if err != nil {
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

// logf notes something for the setup page's activity list.
func (a *app) logf(format string, args ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log = append(a.log, logLine{time.Now().Unix(), fmt.Sprintf(format, args...)})
	if len(a.log) > 60 {
		a.log = a.log[len(a.log)-60:]
	}
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
