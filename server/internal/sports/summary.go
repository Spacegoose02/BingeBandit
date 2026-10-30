package sports

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Summary is everything the Sports Hub shows about one game: its line score,
// the two teams' stats side by side, each team's player stats, the leaders,
// and the standings of the teams' groups.
type Summary struct {
	Game      Game          `json:"game"`
	Stats     []StatLine    `json:"stats"`
	Players   []PlayerTable `json:"players"`
	Leaders   []LeaderLine  `json:"leaders"`
	Standings []Standings   `json:"standings"`
}

// StatLine is one team stat for both sides ("Total Yards", "312", "289").
type StatLine struct {
	Label string `json:"label"`
	Away  string `json:"away"`
	Home  string `json:"home"`
}

// PlayerTable is one kind of player stats (passing, batting) for one team.
type PlayerTable struct {
	Team   string      `json:"team"` // team id
	Title  string      `json:"title"`
	Labels []string    `json:"labels"`
	Rows   []PlayerRow `json:"rows"`
}

type PlayerRow struct {
	Name     string   `json:"name"`
	Position string   `json:"position,omitempty"`
	Stats    []string `json:"stats"`
}

// LeaderLine is a category's leader on each side ("Passing Yards").
type LeaderLine struct {
	Title string  `json:"title"`
	Away  *Leader `json:"away,omitempty"`
	Home  *Leader `json:"home,omitempty"`
}

// Standings is one group's table (a division, a conference, a league).
type Standings struct {
	Title  string         `json:"title"`
	Labels []string       `json:"labels"`
	Rows   []StandingsRow `json:"rows"`
}

type StandingsRow struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Logo  string   `json:"logo,omitempty"`
	Stats []string `json:"stats"`
}

// standingColumns are the standings columns worth a TV's width, in the
// order they show, whichever the sport has.
var standingColumns = []string{"W", "L", "T", "D", "OTL", "PCT", "GB", "GP", "PTS", "GD", "STRK"}

type espnStat struct {
	Abbreviation string `json:"abbreviation"`
	DisplayValue string `json:"displayValue"`
}

type espnSummary struct {
	Header struct {
		ID           string            `json:"id"`
		Competitions []espnCompetition `json:"competitions"`
	} `json:"header"`
	Boxscore struct {
		Teams []struct {
			Team       espnTeam `json:"team"`
			HomeAway   string   `json:"homeAway"`
			Statistics []struct {
				Label        string `json:"label"`
				DisplayValue string `json:"displayValue"`
				// Baseball groups them (batting, pitching).
				Stats []struct {
					Label        string `json:"label"`
					DisplayValue string `json:"displayValue"`
				} `json:"stats"`
			} `json:"statistics"`
		} `json:"teams"`
		Players []struct {
			Team       espnTeam `json:"team"`
			Statistics []struct {
				Name     string   `json:"name"`
				Text     string   `json:"text"`
				Labels   []string `json:"labels"`
				Athletes []struct {
					Athlete espnAthlete `json:"athlete"`
					Stats   []string    `json:"stats"`
				} `json:"athletes"`
			} `json:"statistics"`
		} `json:"players"`
	} `json:"boxscore"`
	Leaders []struct {
		Team    espnTeam      `json:"team"`
		Leaders []espnLeaders `json:"leaders"`
	} `json:"leaders"`
	Standings struct {
		Groups []struct {
			Header    string `json:"header"`
			Standings struct {
				Entries []struct {
					Team string `json:"team"`
					ID   string `json:"id"`
					Logo []struct {
						Href string `json:"href"`
					} `json:"logo"`
					Stats []espnStat `json:"stats"`
				} `json:"entries"`
			} `json:"standings"`
		} `json:"groups"`
	} `json:"standings"`
}

// Summary reads one game's box score, stats, leaders and standings, cached
// for 15 seconds while it's on and ten minutes otherwise.
func (c *Client) Summary(ctx context.Context, league, id string) (*Summary, error) {
	l := LeagueByID(league)
	if l == nil {
		return nil, ErrUnknownLeague
	}
	key := league + "/" + id
	now := c.Now()
	c.mu.Lock()
	if e, ok := c.summaries[key]; ok && now.Before(e.exp) {
		c.mu.Unlock()
		return e.v, nil
	}
	c.mu.Unlock()
	var body espnSummary
	if err := c.get(ctx, BaseURL+"/"+l.path+"/summary?event="+url.QueryEscape(id), &body); err != nil {
		return nil, err
	}
	out := body.summary(league)
	ttl := 10 * time.Minute
	if out.Game.State == "in" {
		ttl = 15 * time.Second
	}
	c.mu.Lock()
	if c.summaries == nil {
		c.summaries = map[string]cached[*Summary]{}
	}
	c.summaries[key] = cached[*Summary]{out, now.Add(ttl)}
	if len(c.summaries) > 200 {
		for k, e := range c.summaries {
			if now.After(e.exp) {
				delete(c.summaries, k)
			}
		}
	}
	c.mu.Unlock()
	return out, nil
}

func (b espnSummary) summary(league string) *Summary {
	out := &Summary{Stats: []StatLine{}, Players: []PlayerTable{}, Leaders: []LeaderLine{}, Standings: []Standings{}}
	if len(b.Header.Competitions) > 0 {
		comp := b.Header.Competitions[0]
		e := espnEvent{ID: b.Header.ID, Date: comp.Date, Competitions: b.Header.Competitions}
		if comp.Status != nil {
			e.Status = *comp.Status
		}
		out.Game = e.game(league)
		out.Game.Name = out.Game.Away.Name + " at " + out.Game.Home.Name
		out.Game.Short = out.Game.Away.Abbr + " @ " + out.Game.Home.Abbr
	}
	homeID, awayID := out.Game.Home.ID, out.Game.Away.ID

	// Team stats, matched up by label in the home side's order.
	sides := map[string][][2]string{}
	var order []string
	for _, t := range b.Boxscore.Teams {
		side := "away"
		if t.HomeAway == "home" || (t.HomeAway == "" && t.Team.ID == homeID) {
			side = "home"
		}
		for _, st := range t.Statistics {
			if len(st.Stats) > 0 {
				for _, x := range st.Stats {
					sides[side] = append(sides[side], [2]string{x.Label, x.DisplayValue})
				}
			} else {
				sides[side] = append(sides[side], [2]string{st.Label, st.DisplayValue})
			}
		}
	}
	values := map[string]*StatLine{}
	for _, side := range []string{"home", "away"} {
		for _, kv := range sides[side] {
			if kv[0] == "" {
				continue
			}
			sl := values[kv[0]]
			if sl == nil {
				sl = &StatLine{Label: kv[0]}
				values[kv[0]] = sl
				order = append(order, kv[0])
			}
			if side == "home" {
				sl.Home = kv[1]
			} else {
				sl.Away = kv[1]
			}
		}
	}
	for _, label := range order {
		out.Stats = append(out.Stats, *values[label])
	}

	// Players: away first, as the line score reads.
	for _, want := range []string{awayID, homeID} {
		for _, p := range b.Boxscore.Players {
			if p.Team.ID != want {
				continue
			}
			for _, st := range p.Statistics {
				tbl := PlayerTable{Team: want, Title: firstNonEmpty(st.Text, titleCase(st.Name)), Labels: st.Labels, Rows: []PlayerRow{}}
				for _, a := range st.Athletes {
					if len(a.Stats) == 0 {
						continue // didn't play
					}
					tbl.Rows = append(tbl.Rows, PlayerRow{Name: firstNonEmpty(a.Athlete.ShortName, a.Athlete.DisplayName), Position: a.Athlete.Position.Abbreviation, Stats: a.Stats})
				}
				if len(tbl.Rows) > 0 {
					out.Players = append(out.Players, tbl)
				}
			}
		}
	}

	// Leaders, a line per category with both sides.
	byTitle := map[string]*LeaderLine{}
	var titles []string
	for _, t := range b.Leaders {
		for _, cat := range t.Leaders {
			if len(cat.Leaders) == 0 {
				continue
			}
			title := firstNonEmpty(cat.DisplayName, titleCase(cat.Name))
			ll := byTitle[title]
			if ll == nil {
				ll = &LeaderLine{Title: title}
				byTitle[title] = ll
				titles = append(titles, title)
			}
			ld := cat.Leaders[0].Athlete.leader(cat.Leaders[0].DisplayValue)
			if t.Team.ID == homeID {
				ll.Home = ld
			} else {
				ll.Away = ld
			}
		}
	}
	for _, t := range titles {
		out.Leaders = append(out.Leaders, *byTitle[t])
	}

	// Standings: the columns the first row has, in standingColumns' order.
	for _, g := range b.Standings.Groups {
		entries := g.Standings.Entries
		if len(entries) == 0 {
			continue
		}
		var cols []string
		for _, c := range standingColumns {
			if slices.ContainsFunc(entries[0].Stats, func(s espnStat) bool { return s.Abbreviation == c }) {
				cols = append(cols, c)
			}
		}
		tbl := Standings{Title: g.Header, Labels: cols, Rows: []StandingsRow{}}
		for _, e := range entries {
			row := StandingsRow{ID: e.ID, Name: e.Team}
			if len(e.Logo) > 0 {
				row.Logo = e.Logo[0].Href
			}
			for _, c := range cols {
				v := ""
				for _, s := range e.Stats {
					if s.Abbreviation == c {
						v = s.DisplayValue
						break
					}
				}
				row.Stats = append(row.Stats, v)
			}
			tbl.Rows = append(tbl.Rows, row)
		}
		out.Standings = append(out.Standings, tbl)
	}
	return out
}

// titleCase turns ESPN's keys ("passingYards", "batting") into labels.
func titleCase(key string) string {
	var b strings.Builder
	for i, r := range key {
		if i == 0 {
			b.WriteString(strings.ToUpper(string(r)))
			continue
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
