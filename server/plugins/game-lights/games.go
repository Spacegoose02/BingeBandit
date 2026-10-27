package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
		UserID int64  `json:"userId"`
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

// game is one game being followed for the people who follow its teams.
type game struct {
	id, league, sport, label string
	sides                    map[string]Team // both teams, by id
	fans                     []fan
	lastSeen                 time.Time
	cancel                   context.CancelFunc
	// playsOK is set once ESPN's play feed answers; until then (or if it
	// stops) football falls back to score changes.
	playsOK  bool
	failures int
	// The newest play (or score) and when ESPN reported it, so people can
	// measure how far their TV runs behind.
	lastAt   time.Time
	lastText string
}

// fan is someone the plugin follows a team for.
type fan struct {
	userID, user, team string
}

// teams lists the teams the game's fans follow, each once.
func (g *game) teams() []string {
	var out []string
	for _, f := range g.fans {
		if !contains(out, f.team) {
			out = append(out, f.team)
		}
	}
	return out
}

// Follower keeps the games of followed teams that are on, reading plays
// for football and score changes for everything else.
type Follower struct {
	a *app
	// every is how often a football game's plays are read.
	every time.Duration

	mu    sync.Mutex
	games map[string]*game
}

// fansFor lists who to light up for: the fans among the people the plugin
// follows (everyone, unless the setting names some).
func (f *Follower) fansFor(ctx context.Context, ev sportsEvent) []fan {
	who := strings.ToLower(f.a.setting(ctx, "users"))
	var names []string
	for _, n := range strings.Split(who, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	people := f.a.conf.get().People
	var out []fan
	for _, fn := range ev.Fans {
		if len(names) > 0 && !contains(names, strings.ToLower(fn.User)) {
			continue
		}
		if !people[strconv.FormatInt(fn.UserID, 10)].Enabled {
			continue // they haven't turned Game Lights on
		}
		if fn.TeamID == ev.Game.Home.ID || fn.TeamID == ev.Game.Away.ID {
			out = append(out, fan{strconv.FormatInt(fn.UserID, 10), fn.User, fn.TeamID})
		}
	}
	return out
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
	fans := f.fansFor(ctx, ev)
	if len(fans) == 0 {
		return
	}
	g, playsOK := f.follow(ev, fans)
	switch name {
	case "sports.score":
		if g.sport == "football" && playsOK || ev.Scorer == "" {
			return
		}
		label := fmt.Sprintf("%s %s–%s", ev.Game.Short, ev.Game.Away.Score, ev.Game.Home.Score)
		f.seen(g, label)
		kind := map[string]string{"hockey": "goal", "soccer": "goal", "baseball": "run", "football": "score"}[g.sport]
		if kind == "" {
			return // basketball scores too often to flash for each basket
		}
		f.fireAll(g, func(team string) (Event, bool) {
			mine := ev.Scorer == team
			return Event{Kind: kind, Mine: mine, Good: mine}, true
		}, label)
	case "sports.game.end":
		f.drop(g.id)
		winner := ""
		for _, sd := range []side{ev.Game.Home, ev.Game.Away} {
			if sd.Winner {
				winner = sd.ID
			}
		}
		if !contains(g.teams(), winner) {
			f.a.logf("%s: %s", ev.Game.Short, ev.Game.Detail)
			return
		}
		f.fireAll(g, func(team string) (Event, bool) {
			return Event{Kind: "win", Good: true}, team == winner
		}, g.sides[winner].Short+" won")
	}
}

// follow finds or starts following the game, and says whether its plays
// can be read.
func (f *Follower) follow(ev sportsEvent, fans []fan) (*game, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.games == nil {
		f.games = map[string]*game{}
	}
	g := f.games[ev.Game.ID]
	if g == nil {
		g = &game{id: ev.Game.ID, league: ev.Game.League, sport: ev.Sport, label: ev.Game.Short, sides: map[string]Team{}}
		for _, sd := range []side{ev.Game.Home, ev.Game.Away} {
			g.sides[sd.ID] = Team{League: ev.Game.League, ID: sd.ID, Name: sd.Name, Short: sd.Short, Abbr: sd.Abbr, Color: sd.Color, AltColor: sd.AltColor}
		}
		f.games[g.id] = g
		if g.sport == "football" {
			ctx, cancel := context.WithCancel(context.Background())
			g.cancel = cancel
			go f.readPlays(ctx, g)
		}
	}
	before := g.teams()
	g.fans = fans
	for _, t := range g.teams() {
		if contains(before, t) {
			continue
		}
		team := g.sides[t]
		f.a.conf.update(func(c *Config) {
			if c.Teams == nil {
				c.Teams = map[string]Team{}
			}
			c.Teams[team.key()] = team
		})
		f.a.logf("Following %s for the %s", g.label, team.Short)
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

// seen notes the newest play, for measuring a TV's delay.
func (f *Follower) seen(g *game, text string) {
	f.mu.Lock()
	g.lastAt, g.lastText = time.Now(), text
	f.mu.Unlock()
}

// fireAll lights up each team's fans for what happened, as that team sees it.
func (f *Follower) fireAll(g *game, what func(team string) (Event, bool), label string) {
	f.mu.Lock()
	teams := g.teams()
	f.mu.Unlock()
	for _, team := range teams {
		if e, ok := what(team); ok {
			f.fire(g, team, e, label)
		}
	}
}

// fire plays an event's effect for each fan of the team, in their room
// after their delay. Fans sharing a room and a delay get it once.
func (f *Follower) fire(g *game, team string, e Event, label string) {
	key := effectKey(e)
	if key == "" {
		return
	}
	conf := f.a.conf.get()
	if contains(conf.Off, key) {
		f.a.logf("%s: %s (effect turned off)", label, key)
		return
	}
	def := atoi(f.a.setting(context.Background(), "delay_seconds"), 25)
	f.mu.Lock()
	fans := append([]fan{}, g.fans...)
	us, them := g.sides[team], Team{}
	for id, t := range g.sides {
		if id != team {
			them = t
		}
	}
	f.mu.Unlock()
	done := map[string]bool{}
	for _, fn := range fans {
		if fn.team != team {
			continue
		}
		p := conf.People[fn.userID]
		k, pal := key, conf.paletteFor(us, p)
		if !p.lightsFor(us.key()) {
			continue
		}
		if key == "opp_score" {
			switch p.Opponent {
			case "off":
				continue
			case "colour":
				k, pal = "opp_colour", conf.paletteFor(them, p)
			}
		}
		delay := p.delay(def)
		once := fmt.Sprintf("%s|%d|%s|%v", p.Area, delay, k, pal.Hex)
		if done[once] {
			continue
		}
		done[once] = true
		f.a.runnerFor(p.Area).Fire(k, pal, label, time.Duration(delay)*time.Second)
	}
}

// Last is the newest play of a game the person follows, and when ESPN
// reported it.
func (f *Follower) Last(userID string) (time.Time, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var at time.Time
	text := ""
	for _, g := range f.games {
		for _, fn := range g.fans {
			if fn.userID == userID && g.lastAt.After(at) {
				at, text = g.lastAt, g.label+": "+g.lastText
			}
		}
	}
	return at, text
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
			label := truncate(p.Text, 70)
			f.seen(g, label)
			f.fireAll(g, func(team string) (Event, bool) { return classify(p, team) }, label)
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
		var names []string
		for _, t := range g.teams() {
			names = append(names, g.sides[t].Short)
		}
		line := g.label + " for the " + strings.Join(names, " and the ")
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
