package sports

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Change is something that happened in a followed game since the last look.
type Change struct {
	Kind string // start | score | end
	Game Game
	// Scorer is the team whose score went up (score only; empty if both did).
	Scorer string
	Prev   Game
}

// Tracker looks at the scoreboards of the teams people follow: every half
// minute while one of their games is on or about to start, every few
// minutes otherwise.
type Tracker struct {
	Client *Client
	// Follows lists the followed team ids in each league.
	Follows func() (map[string]map[string]bool, error)
	// On gets what changed, and every followed game from yesterday to a
	// week ahead, after each look.
	On func(ctx context.Context, changes []Change, games []Game)

	mu   sync.Mutex
	last map[string]Game
}

const (
	lookOften  = 30 * time.Second
	lookSeldom = 5 * time.Minute
)

// Look reads the scoreboards once and says how long to wait before the next look.
func (t *Tracker) Look(ctx context.Context) time.Duration {
	follows, err := t.Follows()
	if err != nil || len(follows) == 0 {
		return lookSeldom
	}
	now := t.Client.Now()
	var games []Game
	for league, teams := range follows {
		if LeagueByID(league) == nil {
			continue // a league the hub has since dropped
		}
		all, err := t.Client.Games(ctx, league, 1, 7)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("sports scoreboard", "league", league, "err", err)
			}
			continue
		}
		for _, g := range all {
			if teams[g.Home.ID] || teams[g.Away.ID] {
				games = append(games, g)
			}
		}
	}
	t.mu.Lock()
	changes := diff(t.last, games)
	if t.last == nil {
		t.last = map[string]Game{}
	}
	for _, g := range games {
		t.last[g.ID] = g
	}
	for id, g := range t.last {
		if now.Sub(g.Start) > 4*24*time.Hour {
			delete(t.last, id)
		}
	}
	t.mu.Unlock()
	if t.On != nil {
		t.On(ctx, changes, games)
	}
	wait := lookSeldom
	for _, g := range games {
		if g.State == "in" || (g.State == "pre" && g.Start.Sub(now) < 30*time.Minute && now.Sub(g.Start) < 6*time.Hour) {
			wait = lookOften
		}
	}
	return wait
}

// diff finds games that started, scored or ended. Games seen for the first
// time give nothing, so a restart mid-game doesn't announce old scores.
func diff(last map[string]Game, games []Game) []Change {
	var out []Change
	for _, g := range games {
		prev, ok := last[g.ID]
		if !ok {
			continue
		}
		switch {
		case prev.State == "pre" && g.State == "in":
			out = append(out, Change{Kind: "start", Game: g, Prev: prev})
		case prev.State != "post" && g.State == "post":
			out = append(out, Change{Kind: "end", Game: g, Prev: prev})
			continue
		}
		if g.State != "in" {
			continue
		}
		home, away := score(g.Home.Score) > score(prev.Home.Score), score(g.Away.Score) > score(prev.Away.Score)
		if home || away {
			c := Change{Kind: "score", Game: g, Prev: prev}
			if home && !away {
				c.Scorer = g.Home.ID
			} else if away && !home {
				c.Scorer = g.Away.ID
			}
			out = append(out, c)
		}
	}
	return out
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
