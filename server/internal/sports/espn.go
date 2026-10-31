// Package sports follows teams on ESPN's public scoreboards: the leagues
// people can pick teams from, each league's teams, and the games on a day.
// ESPN's endpoints are unofficial and need no key; answers are cached so a
// household browsing the Sports page doesn't hammer them.
package sports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // ESPN's days are US Eastern days, on any server
)

// BaseURL is ESPN's site API; tests point it at a fake.
var BaseURL = "https://site.api.espn.com/apis/site/v2/sports"

// League is one of the leagues people can follow.
type League struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Sport string `json:"sport"` // football | basketball | baseball | hockey | soccer | rugby | cricket | ...
	path  string
	// groups asks for every game: college scoreboards otherwise only list
	// a featured few (80 is FBS football, 50 is Division I basketball).
	groups string
}

// Leagues are every league the Sports Hub shows, in the order its rows
// appear. UFC fights are games between two fighters; NASCAR races are games
// with a running order (Race) instead of two sides.
var Leagues = []League{
	{ID: "nfl", Name: "NFL", Sport: "football", path: "football/nfl"},
	{ID: "college-football", Name: "College football", Sport: "football", path: "football/college-football", groups: "80"},
	{ID: "ufl", Name: "UFL", Sport: "football", path: "football/ufl"},
	{ID: "cfl", Name: "CFL", Sport: "football", path: "football/cfl"},
	{ID: "nba", Name: "NBA", Sport: "basketball", path: "basketball/nba"},
	{ID: "wnba", Name: "WNBA", Sport: "basketball", path: "basketball/wnba"},
	{ID: "mens-college-basketball", Name: "Men's college basketball", Sport: "basketball", path: "basketball/mens-college-basketball", groups: "50"},
	{ID: "womens-college-basketball", Name: "Women's college basketball", Sport: "basketball", path: "basketball/womens-college-basketball", groups: "50"},
	{ID: "mlb", Name: "MLB", Sport: "baseball", path: "baseball/mlb"},
	{ID: "college-baseball", Name: "College baseball", Sport: "baseball", path: "baseball/college-baseball"},
	{ID: "nhl", Name: "NHL", Sport: "hockey", path: "hockey/nhl"},
	{ID: "mens-college-hockey", Name: "Men's college hockey", Sport: "hockey", path: "hockey/mens-college-hockey"},
	{ID: "mls", Name: "MLS", Sport: "soccer", path: "soccer/usa.1"},
	// UFC: each fight on a card is a game, the fighters its two sides.
	{ID: "ufc", Name: "UFC", Sport: "mma", path: "mma/ufc"},
	// NASCAR Cup Series: each race is a game with its running order.
	{ID: "nascar", Name: "NASCAR Cup Series", Sport: "racing", path: "racing/nascar-premier"},
}

// LeagueByID finds a league, or nil.
func LeagueByID(id string) *League {
	for i := range Leagues {
		if Leagues[i].ID == id {
			return &Leagues[i]
		}
	}
	return nil
}

// Team is a team in a league, with ESPN's colours (hex without '#').
type Team struct {
	League   string `json:"league"`
	ID       string `json:"id"`
	Name     string `json:"name"`      // Virginia Tech Hokies
	Short    string `json:"shortName"` // Hokies
	Abbr     string `json:"abbr"`      // VT
	Logo     string `json:"logo,omitempty"`
	Color    string `json:"color,omitempty"`
	AltColor string `json:"altColor,omitempty"`
}

// Side is a team in one game.
type Side struct {
	Team
	Home   bool   `json:"home"`
	Score  string `json:"score,omitempty"`
	Winner bool   `json:"winner,omitempty"`
	Record string `json:"record,omitempty"`
	// Lines are the side's score in each period (quarter, inning, half).
	Lines []string `json:"lines,omitempty"`
	// Leader is the side's standout player so far, or before a game its
	// probable starter.
	Leader *Leader `json:"leader,omitempty"`
}

// Driver is a place in a race.
type Driver struct {
	Place  int    `json:"place"`
	Name   string `json:"name"`
	Short  string `json:"shortName"`
	Number string `json:"number,omitempty"` // the car's
	Winner bool   `json:"winner,omitempty"`
}

// Leader is a player and what they've done ("1 REC, 16 YDS").
type Leader struct {
	Name     string `json:"name"`      // Malik Washington
	Short    string `json:"shortName"` // M. Washington
	Position string `json:"position,omitempty"`
	Value    string `json:"value,omitempty"`
}

// Game is one game on a scoreboard.
type Game struct {
	ID     string    `json:"id"`
	League string    `json:"league"`
	Name   string    `json:"name"`      // Miami Hurricanes at Virginia Tech Hokies
	Short  string    `json:"shortName"` // MIA @ VT
	Start  time.Time `json:"start"`
	State  string    `json:"state"`  // pre | in | post
	Detail string    `json:"detail"` // "Final", "3rd Qtr 4:12", "Sat, Oct 25 at 7:30 PM EDT"
	Period int       `json:"period,omitempty"`
	Clock  string    `json:"clock,omitempty"`
	Home   Side      `json:"home"`
	Away   Side      `json:"away"`
	// Race is a race's running order (finishing order once it's over),
	// leader first; racing games have no Home or Away.
	Race []Driver `json:"race,omitempty"`
	// Networks showing it, e.g. ESPN or FOX.
	Broadcasts []string `json:"broadcasts,omitempty"`
	Venue      string   `json:"venue,omitempty"`
}

// Has reports whether a team plays in the game.
func (g *Game) Has(teamID string) bool { return g.Home.ID == teamID || g.Away.ID == teamID }

// Sides returns the team's side and the opponent's.
func (g *Game) Sides(teamID string) (us, them *Side) {
	if g.Home.ID == teamID {
		return &g.Home, &g.Away
	}
	return &g.Away, &g.Home
}

// Client reads ESPN, caching answers.
type Client struct {
	HTTP *http.Client
	Now  func() time.Time

	mu     sync.Mutex
	teams  map[string]cached[[]Team]
	boards map[string]cached[[]Game]
	// summaries are game summaries by league/id.
	summaries map[string]cached[*Summary]
}

type cached[T any] struct {
	v   T
	exp time.Time
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, Now: time.Now,
		teams: map[string]cached[[]Team]{}, boards: map[string]cached[[]Game]{}, summaries: map[string]cached[*Summary]{}}
}

var eastern, _ = time.LoadLocation("America/New_York")

// Day is the ESPN day (US Eastern) a time falls on, as YYYYMMDD.
func Day(t time.Time) string { return t.In(eastern).Format("20060102") }

var ErrUnknownLeague = errors.New("unknown league")

func (c *Client) get(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ESPN: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ESPN: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}

type espnTeam struct {
	ID               string `json:"id"`
	DisplayName      string `json:"displayName"`
	ShortDisplayName string `json:"shortDisplayName"`
	Abbreviation     string `json:"abbreviation"`
	Color            string `json:"color"`
	AlternateColor   string `json:"alternateColor"`
	Logo             string `json:"logo"`
	Logos            []struct {
		Href string `json:"href"`
	} `json:"logos"`
}

func (t espnTeam) team(league string) Team {
	logo := t.Logo
	if logo == "" && len(t.Logos) > 0 {
		logo = t.Logos[0].Href
	}
	return Team{League: league, ID: t.ID, Name: t.DisplayName, Short: t.ShortDisplayName, Abbr: t.Abbreviation,
		Logo: logo, Color: strings.TrimPrefix(t.Color, "#"), AltColor: strings.TrimPrefix(t.AlternateColor, "#")}
}

// Teams lists a league's teams by name, cached for half a day.
func (c *Client) Teams(ctx context.Context, league string) ([]Team, error) {
	l := LeagueByID(league)
	if l == nil {
		return nil, ErrUnknownLeague
	}
	if l.Sport == "mma" || l.Sport == "racing" {
		return []Team{}, nil // fighters and drivers, not teams, so nothing to follow
	}
	c.mu.Lock()
	if e, ok := c.teams[league]; ok && c.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.v, nil
	}
	c.mu.Unlock()
	var body struct {
		Sports []struct {
			Leagues []struct {
				Teams []struct {
					Team espnTeam `json:"team"`
				} `json:"teams"`
			} `json:"leagues"`
		} `json:"sports"`
	}
	url := BaseURL + "/" + l.path + "/teams?limit=1000"
	if l.groups != "" {
		url += "&groups=" + l.groups
	}
	if err := c.get(ctx, url, &body); err != nil {
		return nil, err
	}
	out := []Team{}
	for _, s := range body.Sports {
		for _, lg := range s.Leagues {
			for _, t := range lg.Teams {
				if t.Team.ID != "" {
					out = append(out, t.Team.team(league))
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.mu.Lock()
	c.teams[league] = cached[[]Team]{out, c.Now().Add(12 * time.Hour)}
	c.mu.Unlock()
	return out, nil
}

// Team finds one team of a league.
func (c *Client) Team(ctx context.Context, league, id string) (Team, error) {
	teams, err := c.Teams(ctx, league)
	if err != nil {
		return Team{}, err
	}
	for _, t := range teams {
		if t.ID == id {
			return t, nil
		}
	}
	return Team{}, fmt.Errorf("no team %s in %s", id, league)
}

type espnEvent struct {
	ID           string            `json:"id"`
	Date         string            `json:"date"`
	Name         string            `json:"name"`
	ShortName    string            `json:"shortName"`
	Competitions []espnCompetition `json:"competitions"`
	Status       espnStatus        `json:"status"`
	Circuit      struct {
		FullName string `json:"fullName"`
	} `json:"circuit"`
}

type espnStatus struct {
	DisplayClock string `json:"displayClock"`
	Period       int    `json:"period"`
	Type         struct {
		State       string `json:"state"`
		Detail      string `json:"detail"`
		ShortDetail string `json:"shortDetail"`
	} `json:"type"`
}

type espnCompetition struct {
	ID   string `json:"id"`
	Type struct {
		Abbreviation string `json:"abbreviation"` // racing: FP1, Qual, Race
	} `json:"type"`
	Date   string      `json:"date"`
	Status *espnStatus `json:"status"` // the game summary's header has it here
	Venue  struct {
		FullName string `json:"fullName"`
	} `json:"venue"`
	Competitors []struct {
		HomeAway string   `json:"homeAway"`
		Winner   bool     `json:"winner"`
		Score    string   `json:"score"`
		Team     espnTeam `json:"team"`
		// Fighters, in MMA, instead of a team.
		Athlete *struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			ShortName   string `json:"shortName"`
			Flag        struct {
				Href string `json:"href"`
			} `json:"flag"`
		} `json:"athlete"`
		Records []struct {
			Summary string `json:"summary"`
		} `json:"records"`
		Record []struct { // the game summary's spelling
			Summary string `json:"summary"`
		} `json:"record"`
		Linescores []struct {
			Value        *float64 `json:"value"`
			DisplayValue string   `json:"displayValue"`
		} `json:"linescores"`
		Leaders []espnLeaders `json:"leaders"`
		Order   int           `json:"order"` // racing: the place
		Vehicle struct {
			Number string `json:"number"`
		} `json:"vehicle"`
		Probables []struct {
			Athlete espnAthlete `json:"athlete"`
			Record  string      `json:"record"`
		} `json:"probables"`
	} `json:"competitors"`
	// Football puts the game's leaders here, each naming their team.
	Leaders    []espnLeaders `json:"leaders"`
	Broadcasts []struct {
		Names []string `json:"names"`
	} `json:"broadcasts"`
}

type espnAthlete struct {
	DisplayName string       `json:"displayName"`
	ShortName   string       `json:"shortName"`
	Position    espnPosition `json:"position"`
}

// espnPosition is a player's position. ESPN mostly sends an object
// ({"abbreviation": "RW"}) but a plain string for some, such as the NHL's
// probable goalies ("G"); either is read, and anything else is ignored
// rather than failing the whole scoreboard.
type espnPosition struct {
	Abbreviation string
}

func (p *espnPosition) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		p.Abbreviation = s
		return nil
	}
	var o struct {
		Abbreviation string `json:"abbreviation"`
	}
	if json.Unmarshal(b, &o) == nil {
		p.Abbreviation = o.Abbreviation
	}
	return nil
}

// espnLeaders is one category ("Passing Yards") and its leaders.
type espnLeaders struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Leaders     []struct {
		DisplayValue string      `json:"displayValue"`
		Athlete      espnAthlete `json:"athlete"`
		Team         struct {
			ID string `json:"id"`
		} `json:"team"`
	} `json:"leaders"`
}

func (a espnAthlete) leader(value string) *Leader {
	if a.DisplayName == "" && a.ShortName == "" {
		return nil
	}
	return &Leader{Name: firstNonEmpty(a.DisplayName, a.ShortName), Short: firstNonEmpty(a.ShortName, a.DisplayName), Position: a.Position.Abbreviation, Value: value}
}

// firstLeader is the top player of the first category with one, from
// teamID's side when teamID is set.
func firstLeader(cats []espnLeaders, teamID string) *Leader {
	for _, c := range cats {
		for _, l := range c.Leaders {
			if teamID != "" && l.Team.ID != teamID {
				continue
			}
			if ld := l.Athlete.leader(l.DisplayValue); ld != nil {
				return ld
			}
		}
	}
	return nil
}

func lineScore(v *float64, display string) string {
	if display != "" {
		return display
	}
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

// race keeps a race weekend's race (not practice or qualifying) as the
// event's only competition, so its status and start are the race's.
func (e espnEvent) race() espnEvent {
	if len(e.Competitions) == 0 {
		return e
	}
	c := e.Competitions[len(e.Competitions)-1]
	for _, x := range e.Competitions {
		if strings.EqualFold(x.Type.Abbreviation, "Race") {
			c = x
		}
	}
	e.Competitions = []espnCompetition{c}
	if c.Status != nil {
		e.Status = *c.Status
	}
	e.Date = firstNonEmpty(c.Date, e.Date)
	return e
}

// bouts splits a fight card into one event per fight, each with its own
// status and start, so every fight is a game. The main event comes first.
func (e espnEvent) bouts() []espnEvent {
	out := make([]espnEvent, 0, len(e.Competitions))
	for i := len(e.Competitions) - 1; i >= 0; i-- {
		c := e.Competitions[i]
		b := espnEvent{ID: e.ID + "-" + firstNonEmpty(c.ID, strconv.Itoa(i)), Date: firstNonEmpty(c.Date, e.Date), Name: e.Name, ShortName: e.ShortName,
			Competitions: []espnCompetition{c}, Status: e.Status}
		if c.Status != nil {
			b.Status = *c.Status
		}
		out = append(out, b)
	}
	return out
}

// parseTime reads ESPN's dates, which drop the seconds ("2026-10-25T23:30Z").
func parseTime(s string) time.Time {
	for _, f := range []string{time.RFC3339, "2006-01-02T15:04Z07:00"} {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (e espnEvent) game(league string) Game {
	g := Game{ID: e.ID, League: league, Name: e.Name, Short: e.ShortName, Start: parseTime(e.Date),
		State: e.Status.Type.State, Detail: e.Status.Type.Detail, Period: e.Status.Period}
	if g.State == "in" {
		g.Clock = e.Status.DisplayClock
		g.Detail = firstNonEmpty(e.Status.Type.ShortDetail, e.Status.Type.Detail)
	}
	if len(e.Competitions) == 0 {
		return g
	}
	comp := e.Competitions[0]
	g.Venue = firstNonEmpty(comp.Venue.FullName, e.Circuit.FullName)
	if l := LeagueByID(league); l != nil && l.Sport == "racing" {
		for _, c := range comp.Competitors {
			if c.Athlete == nil {
				continue
			}
			g.Race = append(g.Race, Driver{Place: c.Order, Name: c.Athlete.DisplayName, Short: firstNonEmpty(c.Athlete.ShortName, c.Athlete.DisplayName),
				Number: c.Vehicle.Number, Winner: c.Winner})
		}
		sort.SliceStable(g.Race, func(i, j int) bool { return g.Race[i].Place < g.Race[j].Place })
		switch {
		case g.State == "pre":
			g.Race = nil // the entry list, in no order that means anything yet
		case len(g.Race) > 10:
			g.Race = g.Race[:10]
		}
		g.addBroadcasts(comp)
		return g
	}
	for i, c := range comp.Competitors {
		s := Side{Team: c.Team.team(league), Home: c.HomeAway == "home", Score: c.Score, Winner: c.Winner}
		if c.Athlete != nil {
			s.Team = Team{League: league, ID: c.Athlete.ID, Name: c.Athlete.DisplayName, Short: c.Athlete.ShortName, Logo: c.Athlete.Flag.Href}
			s.Home = i == 0 // the first named is billed first; shown second, like a home side
		}
		if len(c.Records) > 0 {
			s.Record = c.Records[0].Summary
		} else if len(c.Record) > 0 {
			s.Record = c.Record[0].Summary
		}
		if g.State == "pre" {
			s.Score = ""
			if len(c.Probables) > 0 {
				s.Leader = c.Probables[0].Athlete.leader(c.Probables[0].Record)
			}
		} else {
			for _, l := range c.Linescores {
				s.Lines = append(s.Lines, lineScore(l.Value, l.DisplayValue))
			}
			s.Leader = firstLeader(c.Leaders, "")
			if s.Leader == nil {
				s.Leader = firstLeader(comp.Leaders, s.ID)
			}
		}
		if s.Home {
			g.Home = s
		} else {
			g.Away = s
		}
	}
	g.addBroadcasts(comp)
	return g
}

func (g *Game) addBroadcasts(comp espnCompetition) {
	seen := map[string]bool{}
	for _, b := range comp.Broadcasts {
		for _, n := range b.Names {
			if n != "" && !seen[n] {
				seen[n] = true
				g.Broadcasts = append(g.Broadcasts, n)
			}
		}
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// Scoreboard lists a league's games on an ESPN day (YYYYMMDD). Live days are
// cached for 20 seconds, other recent days for a few minutes, and days long
// past or far ahead for an hour.
func (c *Client) Scoreboard(ctx context.Context, league, day string) ([]Game, error) {
	l := LeagueByID(league)
	if l == nil {
		return nil, ErrUnknownLeague
	}
	key := league + "/" + day
	now := c.Now()
	c.mu.Lock()
	if e, ok := c.boards[key]; ok && now.Before(e.exp) {
		c.mu.Unlock()
		return e.v, nil
	}
	c.mu.Unlock()
	url := BaseURL + "/" + l.path + "/scoreboard?limit=1000&dates=" + day
	if l.groups != "" {
		url += "&groups=" + l.groups
	}
	var body struct {
		Events []espnEvent `json:"events"`
	}
	if err := c.get(ctx, url, &body); err != nil {
		return nil, err
	}
	out := make([]Game, 0, len(body.Events))
	ttl := time.Hour
	if day >= Day(now.Add(-36*time.Hour)) && day <= Day(now.Add(48*time.Hour)) {
		ttl = 5 * time.Minute
	}
	var events []espnEvent
	for _, e := range body.Events {
		if l.Sport == "mma" {
			events = append(events, e.bouts()...)
		} else if l.Sport == "racing" {
			events = append(events, e.race())
		} else {
			events = append(events, e)
		}
	}
	for _, e := range events {
		g := e.game(league)
		if g.State == "in" || (g.State == "pre" && g.Start.Sub(now) < 20*time.Minute && now.Sub(g.Start) < 6*time.Hour) {
			ttl = 20 * time.Second
		}
		out = append(out, g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	c.mu.Lock()
	c.boards[key] = cached[[]Game]{out, now.Add(ttl)}
	// Drop old entries now and then.
	if len(c.boards) > 400 {
		for k, e := range c.boards {
			if now.After(e.exp) {
				delete(c.boards, k)
			}
		}
	}
	c.mu.Unlock()
	return out, nil
}

// Games lists a league's games from before days ago through after days
// ahead, oldest first. Days that fail are skipped unless all do.
func (c *Client) Games(ctx context.Context, league string, before, after int) ([]Game, error) {
	now := c.Now()
	var out []Game
	var firstErr error
	seen := map[string]bool{}
	ok := false
	for d := -before; d <= after; d++ {
		games, err := c.Scoreboard(ctx, league, Day(now.AddDate(0, 0, d)))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if errors.Is(err, ErrUnknownLeague) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		ok = true
		for _, g := range games {
			if !seen[g.ID] {
				seen[g.ID] = true
				out = append(out, g)
			}
		}
	}
	if !ok && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
