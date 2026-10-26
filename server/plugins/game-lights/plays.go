package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ESPN's play-by-play: the core API first, then the site API's game
// summary. Both are unofficial; tests point them at a fake.
var (
	espnCore = "https://sports.core.api.espn.com/v2/sports"
	espnSite = "https://site.api.espn.com/apis/site/v2/sports"
)

var espnClient = &http.Client{Timeout: 10 * time.Second}

// teamRef is a team as plays give it: {id} on the site API, {$ref} on the core API.
type teamRef struct {
	ID  string `json:"id"`
	Ref string `json:"$ref"`
}

var reTeamRef = regexp.MustCompile(`teams/(\d+)`)

func (t *teamRef) id() string {
	if t == nil {
		return ""
	}
	if t.ID != "" {
		return t.ID
	}
	if m := reTeamRef.FindStringSubmatch(t.Ref); m != nil {
		return m[1]
	}
	return ""
}

// Play is one play, in the shape both ESPN APIs share.
type Play struct {
	ID   string `json:"id"`
	Type struct {
		Text string `json:"text"`
	} `json:"type"`
	Text        string   `json:"text"`
	ScoringPlay bool     `json:"scoringPlay"`
	Team        *teamRef `json:"team"`
	Start       struct {
		Team *teamRef `json:"team"`
	} `json:"start"`
	StatYardage float64 `json:"statYardage"`
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := espnClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 160))
		return fmt.Errorf("ESPN %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

// fetchPlays reads a football game's plays, oldest first.
func fetchPlays(ctx context.Context, league, eventID string) ([]Play, error) {
	var core struct {
		Items []Play `json:"items"`
	}
	err := getJSON(ctx, fmt.Sprintf("%s/football/leagues/%s/events/%s/competitions/%s/plays?limit=1000", espnCore, league, eventID, eventID), &core)
	if err == nil && len(core.Items) > 0 {
		return core.Items, nil
	}
	var sum struct {
		Drives struct {
			Previous []struct {
				Plays []Play `json:"plays"`
			} `json:"previous"`
			Current *struct {
				Plays []Play `json:"plays"`
			} `json:"current"`
		} `json:"drives"`
		Plays []Play `json:"plays"`
	}
	if err := getJSON(ctx, fmt.Sprintf("%s/football/%s/summary?event=%s", espnSite, league, eventID), &sum); err != nil {
		return nil, err
	}
	var out []Play
	for _, d := range sum.Drives.Previous {
		out = append(out, d.Plays...)
	}
	if c := sum.Drives.Current; c != nil {
		out = append(out, c.Plays...)
	}
	if len(out) == 0 {
		out = sum.Plays
	}
	return out, nil
}

var (
	reTouchdown   = regexp.MustCompile(`touchdown`)
	reFieldGoal   = regexp.MustCompile(`field goal`)
	reSafety      = regexp.MustCompile(`safety`)
	reIntercepted = regexp.MustCompile(`intercepted`)
	reDowns       = regexp.MustCompile(`turnover on downs`)
	reFumbles     = regexp.MustCompile(`fumbles`)
	reRecoveredBy = regexp.MustCompile(`recovered by`)
	reSacked      = regexp.MustCompile(`\bsacked\b`)
	rePassRush    = regexp.MustCompile(`pass|rush`)
)

// classify turns a football play into an event for the followed team, or
// reports false for plays that light nothing.
func classify(p Play, team string) (Event, bool) {
	typ, text := strings.ToLower(p.Type.Text), strings.ToLower(p.Text)
	offense := p.Start.Team.id()
	if offense == "" {
		offense = p.Team.id()
	}
	scorer := p.Team.id()
	if scorer == "" {
		scorer = offense
	}
	ours := offense == team
	if p.ScoringPlay {
		kind := "score"
		switch {
		case strings.Contains(typ, "touchdown") || reTouchdown.MatchString(text):
			kind = "touchdown"
		case strings.Contains(typ, "field goal") || reFieldGoal.MatchString(text):
			kind = "field_goal"
		case reSafety.MatchString(text):
			kind = "safety"
		}
		return Event{Kind: kind, Mine: scorer == team, Good: scorer == team}, true
	}
	if strings.Contains(typ, "interception") || reIntercepted.MatchString(text) {
		return Event{Kind: "interception", Good: !ours}, true
	}
	if strings.Contains(typ, "turnover on downs") || reDowns.MatchString(text) {
		return Event{Kind: "turnover_on_downs", Good: !ours}, true
	}
	// "Fumble Recovery (Opponent)": the defence got it; "(Own)": the offence kept it.
	if strings.Contains(typ, "fumble") || reFumbles.MatchString(text) {
		if strings.Contains(typ, "recovery (own)") {
			return Event{}, false
		}
		if strings.Contains(typ, "recovery (opponent)") || reRecoveredBy.MatchString(text) {
			return Event{Kind: "fumble_lost", Good: !ours}, true
		}
		return Event{}, false
	}
	if typ == "sack" || reSacked.MatchString(text) {
		return Event{Kind: "sack", Good: !ours}, true
	}
	if p.StatYardage >= 20 && ours && rePassRush.MatchString(typ) {
		return Event{Kind: "big_play", Good: true}, true
	}
	return Event{}, false
}
