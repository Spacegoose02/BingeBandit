package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// sportsEvent is the data of BingeBandit's sports.* events.
type sportsEvent struct {
	Game struct {
		ID     string `json:"id"`
		League string `json:"league"`
		Short  string `json:"shortName"`
		Detail string `json:"detail"`
		Home   side   `json:"home"`
		Away   side   `json:"away"`
	} `json:"game"`
	Sport  string `json:"sport"`
	Scorer string `json:"scorer"`
	Fans   []struct {
		User   string `json:"user"`
		TeamID string `json:"teamId"`
	} `json:"fans"`
}

type side struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Short    string `json:"shortName"`
	Abbr     string `json:"abbr"`
	Color    string `json:"color"`
	AltColor string `json:"altColor"`
	Score    string `json:"score"`
	Winner   bool   `json:"winner"`
}

// game is one game being followed for one team.
type game struct {
	id, league, sport, label string
	team                     Team
	lastSeen                 time.Time
	cancel                   context.CancelFunc
	// playsOK is set once ESPN's play feed answers; until then (or if it
	// stops) football falls back to score changes.
	playsOK  bool
	failures int
}

// Follower keeps one game per followed team that is on, reading plays for
// football and score changes for everything else.
type Follower struct {
	a *app
	// every is how often a football game's plays are read.
	every time.Duration

	mu    sync.Mutex
	games map[string]*game
}

// teamFor picks the team to cheer for: the first fan's team among the
// people the plugin follows (everyone, unless the setting names some).
func (f *Follower) teamFor(ctx context.Context, ev sportsEvent) (side, bool) {
	who := strings.ToLower(f.a.setting(ctx, "users"))
	var names []string
	for _, n := range strings.Split(who, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	for _, fan := range ev.Fans {
		if len(names) > 0 && !contains(names, strings.ToLower(fan.User)) {
			continue
		}
		switch fan.TeamID {
		case ev.Game.Home.ID:
			return ev.Game.Home, true
		case ev.Game.Away.ID:
			return ev.Game.Away, true
		}
	}
	return side{}, false
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (f *Follower) onEvent(ctx context.Context, name string, data json.RawMessage) {
	var ev sportsEvent
	if json.Unmarshal(data, &ev) != nil || ev.Game.ID == "" {
		return
	}
	us, ok := f.teamFor(ctx, ev)
	if !ok {
		return
	}
	g, playsOK := f.follow(ev, us)
	switch name {
	case "sports.score":
		if g.sport == "football" && playsOK || ev.Scorer == "" {
			return
		}
		mine := ev.Scorer == us.ID
		kind := map[string]string{"hockey": "goal", "soccer": "goal", "baseball": "run", "football": "score"}[g.sport]
		if kind == "" {
			return // basketball scores too often to flash for each basket
		}
		f.fire(g, Event{Kind: kind, Mine: mine, Good: mine}, fmt.Sprintf("%s %s–%s", ev.Game.Short, ev.Game.Away.Score, ev.Game.Home.Score))
	case "sports.game.end":
		f.drop(g.id)
		if us.Winner {
			f.fire(g, Event{Kind: "win", Good: true}, g.team.Short+" won")
		} else {
			f.a.logf("%s: %s", ev.Game.Short, ev.Game.Detail)
		}
	}
}

// follow finds or starts following the game, and says whether its plays
// can be read.
func (f *Follower) follow(ev sportsEvent, us side) (*game, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.games == nil {
		f.games = map[string]*game{}
	}
	g := f.games[ev.Game.ID]
	if g == nil {
		g = &game{id: ev.Game.ID, league: ev.Game.League, sport: ev.Sport, label: ev.Game.Short,
			team: Team{League: ev.Game.League, ID: us.ID, Name: us.Name, Short: us.Short, Abbr: us.Abbr, Color: us.Color, AltColor: us.AltColor}}
		f.games[g.id] = g
		f.a.conf.update(func(c *Config) {
			if c.Teams == nil {
				c.Teams = map[string]Team{}
			}
			c.Teams[g.team.key()] = g.team
		})
		f.a.logf("Following %s for the %s", g.label, g.team.Short)
		if g.sport == "football" {
			ctx, cancel := context.WithCancel(context.Background())
			g.cancel = cancel
			go f.readPlays(ctx, g)
		}
	}
	g.lastSeen = time.Now()
	return g, g.playsOK
}

func (f *Follower) drop(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if g := f.games[id]; g != nil {
		if g.cancel != nil {
			g.cancel()
		}
		delete(f.games, id)
	}
}

// sweep forgets games BingeBandit stopped calling live (a lost end event).
func (f *Follower) sweep() {
	f.mu.Lock()
	var stale []string
	for id, g := range f.games {
		if time.Since(g.lastSeen) > 5*time.Minute {
			stale = append(stale, id)
		}
	}
	f.mu.Unlock()
	for _, id := range stale {
		f.drop(id)
	}
}

func (f *Follower) fire(g *game, e Event, label string) {
	key := effectKey(e)
	if key == "" {
		return
	}
	conf := f.a.conf.get()
	if contains(conf.Off, key) {
		f.a.logf("%s: %s (effect turned off)", label, key)
		return
	}
	delay := time.Duration(atoi(f.a.setting(context.Background(), "delay_seconds"), 25)) * time.Second
	f.a.runner.Fire(key, conf.palette(g.team), label, delay)
}

// readPlays reads a football game's plays until ctx ends. The first read
// only notes what already happened, so joining mid-game replays nothing.
func (f *Follower) readPlays(ctx context.Context, g *game) {
	seen := map[string]bool{}
	first := true
	for ctx.Err() == nil {
		plays, err := fetchPlays(ctx, g.league, g.id)
		f.mu.Lock()
		if err != nil {
			g.failures++
			if g.failures >= 3 {
				g.playsOK = false
			}
		} else {
			g.failures, g.playsOK = 0, true
		}
		failures := g.failures
		f.mu.Unlock()
		if failures == 3 {
			f.a.logf("%s: can't read plays (%v); following the score instead", g.label, err)
		}
		for _, p := range plays {
			if p.ID == "" || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			if first {
				continue
			}
			if e, ok := classify(p, g.team.ID); ok {
				f.fire(g, e, truncate(p.Text, 70))
			}
		}
		if err == nil {
			first = false
		}
		sleep(ctx, f.every)
	}
}

// Games lists what's being followed, for the setup page.
func (f *Follower) Games() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, g := range f.games {
		line := g.label + " for the " + g.team.Short
		if g.sport == "football" && !g.playsOK && g.failures >= 3 {
			line += " (score only)"
		}
		out = append(out, line)
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
