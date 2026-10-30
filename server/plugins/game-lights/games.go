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
	// score is each side's score at the last event, by team id; nil until
	// the first. Basketball also keeps who's ahead and who's on a run.
	score map[string]int
	court court
}

// court follows a basketball game's lead and scoring runs from its score.
type court struct {
	leader string // the team last ahead; a tie keeps it
	run    string // the team on the current run
	runPts int
	// what the last score change did, for hoops to light
	tookLead, reachedRun string
}

// runPoints is how many unanswered points make a run worth lighting up.
const runPoints = 10

// add takes a new score and what each side scored since the last one.
func (c *court) add(score, d map[string]int) {
	c.tookLead, c.reachedRun = "", ""
	switch len(d) {
	case 1:
		for id, n := range d {
			if id != c.run {
				c.run, c.runPts = id, 0
			}
			if c.runPts < runPoints && c.runPts+n >= runPoints {
				c.reachedRun = id
			}
			c.runPts += n
		}
	case 2: // both scored between looks: no run
		c.run, c.runPts = "", 0
	}
	ahead, most, tied := "", -1, false
	for id, n := range score {
		switch {
		case n > most:
			ahead, most, tied = id, n, false
		case n == most:
			tied = true
		}
	}
	if !tied && ahead != c.leader {
		if c.leader != "" {
			c.tookLead = ahead
		}
		c.leader = ahead
	}
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
// for football and score changes for everything else: goals in hockey and
// soccer, runs and home runs in baseball, lead changes and runs in
// basketball.
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
		d, now := f.points(g, ev)
		if g.sport == "football" && playsOK || len(d) == 0 {
			return
		}
		label := fmt.Sprintf("%s %s–%s", ev.Game.Short, ev.Game.Away.Score, ev.Game.Home.Score)
		f.seen(g, label)
		switch g.sport {
		case "basketball":
			f.hoops(g, label)
		case "baseball":
			go f.runs(g, ev.Game.Home.ID, now, d, label)
		default:
			kind := map[string]string{"hockey": "goal", "soccer": "goal"}[g.sport]
			if kind == "" {
				kind = "score"
			}
			for scorer := range d {
				f.fireAll(g, func(team string) (Event, bool) {
					mine := scorer == team
					return Event{Kind: kind, Mine: mine, Good: mine}, true
				}, label)
			}
		}
	case "sports.game.start", "sports.game.live":
		f.points(g, ev)
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

// points notes the game's new score and says how much each side scored
// since the last event. Joining on a score change, it trusts the scorer
// ESPN's scoreboard named.
func (f *Follower) points(g *game, ev sportsEvent) (d, now map[string]int) {
	now = map[string]int{ev.Game.Home.ID: score(ev.Game.Home.Score), ev.Game.Away.ID: score(ev.Game.Away.Score)}
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := g.score
	g.score = now
	d = map[string]int{}
	switch {
	case prev != nil:
		for id, n := range now {
			if n > prev[id] {
				d[id] = n - prev[id]
			}
		}
	case ev.Scorer != "":
		d[ev.Scorer] = 1
	}
	if g.sport == "basketball" {
		g.court.add(now, d)
	}
	return d, now
}

func score(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// hoops lights a basketball score change: a 10–0 run, or else a lead
// change. Single baskets come too often to flash for.
func (f *Follower) hoops(g *game, label string) {
	f.mu.Lock()
	run, lead := g.court.reachedRun, g.court.tookLead
	f.mu.Unlock()
	kind, by := "scoring_run", run
	if run == "" {
		kind, by = "lead", lead
	}
	if by == "" {
		return
	}
	f.fireAll(g, func(team string) (Event, bool) {
		mine := by == team
		return Event{Kind: kind, Mine: mine, Good: mine}, true
	}, label)
}

// runs lights a baseball score change, as a home run when ESPN's plays say
// one brought the runs in, and as runs otherwise (or if the plays can't be read).
func (f *Follower) runs(g *game, homeID string, now, d map[string]int, label string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	plays, _ := fetchPlays(ctx, "baseball", g.league, g.id)
	homers := homeRuns(plays, homeID, now, d)
	for scorer := range d {
		kind, text := "run", label
		if h, ok := homers[scorer]; ok {
			kind, text = "home_run", truncate(h, 70)
		}
		f.fireAll(g, func(team string) (Event, bool) {
			mine := scorer == team
			return Event{Kind: kind, Mine: mine, Good: mine}, true
		}, text)
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
		for _, s := range p.spots(fn.userID) {
			if s.local {
				f.a.fireLocal(s.box, k, pal, label, time.Duration(delay)*time.Second)
				continue
			}
			once := fmt.Sprintf("%s|%d|%s|%v", s.area, delay, k, pal.Hex)
			if done[once] {
				continue
			}
			done[once] = true
			f.a.runnerFor(s.area).Fire(k, pal, label, time.Duration(delay)*time.Second)
		}
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
		plays, err := fetchPlays(ctx, "football", g.league, g.id)
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
