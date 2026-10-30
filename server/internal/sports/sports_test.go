package sports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeESPN serves one college football game whose state the test changes.
type fakeESPN struct {
	mu         sync.Mutex
	state      string
	home, away string
	start      time.Time
	requests   []string
}

func (f *fakeESPN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.URL.Path+"?"+r.URL.RawQuery)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/teams"):
		fmt.Fprint(w, `{"sports":[{"leagues":[{"teams":[
			{"team":{"id":"259","displayName":"Virginia Tech Hokies","shortDisplayName":"Hokies","abbreviation":"VT","color":"630031","alternateColor":"cf4420","logos":[{"href":"https://a.espncdn.com/vt.png"}]}},
			{"team":{"id":"2390","displayName":"Miami Hurricanes","shortDisplayName":"Hurricanes","abbreviation":"MIA","color":"005030","alternateColor":"f47321","logos":[{"href":"https://a.espncdn.com/mia.png"}]}}]}]}]}`)
	case strings.HasSuffix(r.URL.Path, "/scoreboard"):
		if r.URL.Query().Get("dates") != Day(f.start) {
			fmt.Fprint(w, `{"events":[]}`)
			return
		}
		fmt.Fprintf(w, `{"events":[{"id":"401","date":%q,"name":"Miami Hurricanes at Virginia Tech Hokies","shortName":"MIA @ VT",
			"competitions":[{"venue":{"fullName":"Lane Stadium"},"broadcasts":[{"names":["ACC Network"]}],"competitors":[
				{"homeAway":"home","score":%q,"winner":%v,"team":{"id":"259","displayName":"Virginia Tech Hokies","shortDisplayName":"Hokies","abbreviation":"VT","color":"630031","alternateColor":"cf4420","logo":"https://a.espncdn.com/vt.png"},"records":[{"summary":"5-2"}]},
				{"homeAway":"away","score":%q,"team":{"id":"2390","displayName":"Miami Hurricanes","shortDisplayName":"Hurricanes","abbreviation":"MIA","color":"005030","alternateColor":"f47321","logo":"https://a.espncdn.com/mia.png"}}]}],
			"status":{"displayClock":"4:12","period":3,"type":{"state":%q,"detail":"detail","shortDetail":"3rd 4:12"}}}]}`,
			f.start.UTC().Format("2006-01-02T15:04Z"), f.home, f.state == "post", f.away, f.state)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeESPN) set(state, home, away string) {
	f.mu.Lock()
	f.state, f.home, f.away = state, home, away
	f.mu.Unlock()
}

func newFake(t *testing.T, start time.Time) (*fakeESPN, *Client, *time.Time) {
	f := &fakeESPN{state: "pre", home: "0", away: "0", start: start}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	old := BaseURL
	BaseURL = srv.URL
	t.Cleanup(func() { BaseURL = old })
	now := start.Add(-10 * time.Minute)
	c := NewClient()
	c.Now = func() time.Time { return now }
	return f, c, &now
}

func TestTeamsAndScoreboard(t *testing.T) {
	start := time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC) // 7:30 PM Eastern
	f, c, _ := newFake(t, start)
	teams, err := c.Teams(context.Background(), "college-football")
	if err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 || teams[0].Name != "Miami Hurricanes" || teams[1].Logo != "https://a.espncdn.com/vt.png" || teams[1].Color != "630031" {
		t.Fatalf("teams = %+v", teams)
	}
	if !strings.Contains(f.requests[0], "groups=80") {
		t.Errorf("college football should ask for every FBS team: %s", f.requests[0])
	}
	if _, err := c.Teams(context.Background(), "cricket"); err != ErrUnknownLeague {
		t.Errorf("unknown league: %v", err)
	}

	games, err := c.Scoreboard(context.Background(), "college-football", "20261025")
	if err != nil || len(games) != 1 {
		t.Fatalf("games = %+v, %v", games, err)
	}
	g := games[0]
	if !g.Start.Equal(start) || g.Home.Short != "Hokies" || !g.Home.Home || g.Away.Abbr != "MIA" || g.Home.Score != "" ||
		g.Broadcasts[0] != "ACC Network" || g.Venue != "Lane Stadium" || g.Home.Record != "5-2" {
		t.Fatalf("game = %+v", g)
	}
	// Cached: a second look doesn't ask ESPN again.
	n := len(f.requests)
	c.Scoreboard(context.Background(), "college-football", "20261025")
	if len(f.requests) != n {
		t.Error("scoreboard wasn't cached")
	}
}

func TestTrackerChanges(t *testing.T) {
	start := time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC)
	f, c, now := newFake(t, start)
	var got []Change
	var live int
	tr := &Tracker{Client: c,
		Follows: func() (map[string]map[string]bool, error) {
			return map[string]map[string]bool{"college-football": {"259": true}}, nil
		},
		On: func(_ context.Context, ch []Change, games []Game) {
			got = append(got, ch...)
			for _, g := range games {
				if g.State == "in" {
					live++
				}
			}
		}}
	look := func(state, home, away string) time.Duration {
		f.set(state, home, away)
		*now = now.Add(time.Minute) // past the live cache
		return tr.Look(context.Background())
	}
	if wait := look("pre", "0", "0"); wait != lookOften {
		t.Errorf("a game 10 minutes away should be watched closely, got %v", wait)
	}
	look("in", "0", "0")
	look("in", "7", "0")
	look("in", "7", "0")
	look("in", "10", "3")
	if wait := look("post", "17", "3"); wait != lookSeldom {
		t.Errorf("after the game: %v", wait)
	}
	var kinds []string
	for _, ch := range got {
		kinds = append(kinds, ch.Kind+":"+ch.Scorer)
	}
	if want := "start: score:259 score: end:"; strings.Join(kinds, " ") != want {
		t.Errorf("changes = %q, want %q", strings.Join(kinds, " "), want)
	}
	if live != 4 {
		t.Errorf("live looks = %d", live)
	}

	// A tracker that first sees the game mid-way announces nothing old.
	got = nil
	tr2 := &Tracker{Client: c, Follows: tr.Follows, On: tr.On}
	f.set("in", "21", "3")
	tr2.Look(context.Background())
	if len(got) != 0 {
		t.Errorf("first look announced %+v", got)
	}
}

func TestSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/baseball/mlb/summary" || r.URL.Query().Get("event") != "77" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{
		"header":{"id":"77","competitions":[{"date":"2026-09-20T17:10Z",
			"status":{"period":8,"displayClock":"0:00","type":{"state":"in","detail":"Top 8th","shortDetail":"Top 8th"}},
			"competitors":[
				{"homeAway":"home","score":"5","team":{"id":"6","displayName":"Detroit Tigers","abbreviation":"DET","logos":[{"href":"det.png"}]},
				 "record":[{"summary":"80-70"}],"linescores":[{"displayValue":"0"},{"displayValue":"0"},{"displayValue":"4"}]},
				{"homeAway":"away","score":"1","team":{"id":"27","displayName":"Colorado Rockies","abbreviation":"COL"},
				 "linescores":[{"displayValue":"0"},{"displayValue":"1"},{"displayValue":"0"}]}]}]},
		"boxscore":{
			"teams":[
				{"team":{"id":"27"},"homeAway":"away","statistics":[{"name":"batting","stats":[{"label":"Hits","displayValue":"4"}]}]},
				{"team":{"id":"6"},"homeAway":"home","statistics":[{"name":"batting","stats":[{"label":"Hits","displayValue":"9"}]}]}],
			"players":[
				{"team":{"id":"6"},"statistics":[{"name":"batting","labels":["H-AB","RBI"],"athletes":[
					{"athlete":{"displayName":"Riley Greene","shortName":"R. Greene","position":{"abbreviation":"LF"}},"stats":["2-4","3"]},
					{"athlete":{"displayName":"Bench Guy"},"stats":[]}]}]},
				{"team":{"id":"27"},"statistics":[{"name":"batting","labels":["H-AB","RBI"],"athletes":[
					{"athlete":{"displayName":"Ezequiel Tovar","shortName":"E. Tovar"},"stats":["1-4","1"]}]}]}]},
		"leaders":[
			{"team":{"id":"6"},"leaders":[{"name":"homeRuns","displayName":"Home Runs","leaders":[{"displayValue":"2","athlete":{"displayName":"Riley Greene","shortName":"R. Greene"}}]}]},
			{"team":{"id":"27"},"leaders":[{"name":"homeRuns","displayName":"Home Runs","leaders":[{"displayValue":"1","athlete":{"displayName":"Ezequiel Tovar"}}]}]}],
		"standings":{"groups":[{"header":"AL Central","standings":{"entries":[
			{"team":"Detroit Tigers","id":"6","logo":[{"href":"det.png"}],"stats":[{"abbreviation":"W","displayValue":"80"},{"abbreviation":"L","displayValue":"70"},{"abbreviation":"XYZ","displayValue":"?"},{"abbreviation":"GB","displayValue":"-"}]}]}}]}
		}`)
	}))
	defer srv.Close()
	old := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = old }()
	c := NewClient()
	s, err := c.Summary(context.Background(), "mlb", "77")
	if err != nil {
		t.Fatal(err)
	}
	g := s.Game
	if g.State != "in" || g.Detail != "Top 8th" || g.Home.Score != "5" || g.Home.Record != "80-70" || strings.Join(g.Home.Lines, ",") != "0,0,4" || g.Away.Abbr != "COL" || g.Short != "COL @ DET" {
		t.Errorf("game = %+v", g)
	}
	if len(s.Stats) != 1 || s.Stats[0] != (StatLine{"Hits", "4", "9"}) {
		t.Errorf("stats = %+v", s.Stats)
	}
	if len(s.Players) != 2 || s.Players[0].Team != "27" || s.Players[1].Title != "Batting" || len(s.Players[1].Rows) != 1 || s.Players[1].Rows[0].Position != "LF" {
		t.Errorf("players = %+v", s.Players)
	}
	if len(s.Leaders) != 1 || s.Leaders[0].Home.Short != "R. Greene" || s.Leaders[0].Away.Short != "Ezequiel Tovar" {
		t.Errorf("leaders = %+v", s.Leaders)
	}
	if len(s.Standings) != 1 || strings.Join(s.Standings[0].Labels, ",") != "W,L,GB" || strings.Join(s.Standings[0].Rows[0].Stats, ",") != "80,70,-" {
		t.Errorf("standings = %+v", s.Standings)
	}
	if _, err := c.Summary(context.Background(), "curling", "1"); err != ErrUnknownLeague {
		t.Errorf("unknown league: %v", err)
	}
}

func TestLinesAndLeaders(t *testing.T) {
	var e espnEvent
	if err := json.Unmarshal([]byte(`{"id":"9","date":"2026-10-25T17:00Z","status":{"type":{"state":"in"}},"competitions":[{
		"competitors":[
			{"homeAway":"home","score":"6","team":{"id":"16"},"linescores":[{"value":3},{"value":3}]},
			{"homeAway":"away","score":"0","team":{"id":"15"},"linescores":[{"value":0},{"value":0}]}],
		"leaders":[{"name":"passingYards","leaders":[{"displayValue":"7/13, 55 YDS","team":{"id":"16"},"athlete":{"displayName":"Kyler Murray","shortName":"K. Murray","position":{"abbreviation":"QB"}}}]},
			{"name":"receivingYards","leaders":[{"displayValue":"1 REC, 16 YDS","team":{"id":"15"},"athlete":{"displayName":"Malik Washington","shortName":"M. Washington","position":{"abbreviation":"WR"}}}]}]}]}`), &e); err != nil {
		t.Fatal(err)
	}
	g := e.game("nfl")
	if strings.Join(g.Home.Lines, ",") != "3,3" || g.Home.Leader.Short != "K. Murray" || g.Away.Leader.Value != "1 REC, 16 YDS" || g.Away.Leader.Position != "WR" {
		t.Errorf("game = %+v / %+v", g.Home, g.Away)
	}
}

func TestUFCBouts(t *testing.T) {
	var e espnEvent
	if err := json.Unmarshal([]byte(`{"id":"600","date":"2026-11-01T01:00Z","name":"UFC 320","status":{"type":{"state":"in"}},"competitions":[
		{"id":"1","date":"2026-10-31T23:00Z","status":{"type":{"state":"post","detail":"Final"}},"competitors":[
			{"winner":true,"athlete":{"id":"11","displayName":"Ann Early","shortName":"A. Early"}},{"athlete":{"id":"12","displayName":"Bea Prelim"}}]},
		{"id":"2","date":"2026-11-01T03:00Z","status":{"type":{"state":"pre"}},"competitors":[
			{"athlete":{"id":"21","displayName":"Cy Main","flag":{"href":"us.png"}}},{"athlete":{"id":"22","displayName":"Di Event"}}]}]}`), &e); err != nil {
		t.Fatal(err)
	}
	b := e.bouts()
	if len(b) != 2 {
		t.Fatalf("bouts = %d", len(b))
	}
	main, early := b[0].game("ufc"), b[1].game("ufc")
	if main.ID != "600-2" || main.State != "pre" || main.Home.Name != "Cy Main" || main.Home.Logo != "us.png" || main.Away.ID != "22" {
		t.Errorf("main event = %+v", main)
	}
	if early.State != "post" || !early.Home.Winner || early.Home.Short != "A. Early" {
		t.Errorf("early fight = %+v", early)
	}
}

func TestRace(t *testing.T) {
	var e espnEvent
	if err := json.Unmarshal([]byte(`{"id":"700","date":"2026-11-02T18:00Z","name":"Xfinity 500","circuit":{"fullName":"Martinsville Speedway"},"competitions":[
		{"id":"q","type":{"abbreviation":"Qual"},"date":"2026-11-01T17:00Z","status":{"type":{"state":"post"}},"competitors":[{"order":1,"athlete":{"displayName":"Pole Sitter"}}]},
		{"id":"r","type":{"abbreviation":"Race"},"date":"2026-11-02T19:00Z","status":{"type":{"state":"in","detail":"Lap 120 of 500","shortDetail":"Lap 120"}},
		 "broadcasts":[{"names":["NBC"]}],"competitors":[
			{"order":2,"vehicle":{"number":"24"},"athlete":{"displayName":"Second Driver","shortName":"S. Driver"}},
			{"order":1,"vehicle":{"number":"5"},"athlete":{"displayName":"Lead Driver","shortName":"L. Driver"}}]}]}`), &e); err != nil {
		t.Fatal(err)
	}
	g := e.race().game("nascar")
	if g.State != "in" || g.Detail != "Lap 120" || g.Venue != "Martinsville Speedway" || g.Broadcasts[0] != "NBC" || !g.Start.Equal(time.Date(2026, 11, 2, 19, 0, 0, 0, time.UTC)) {
		t.Errorf("race = %+v", g)
	}
	if len(g.Race) != 2 || g.Race[0].Short != "L. Driver" || g.Race[0].Number != "5" || g.Race[1].Place != 2 || g.Home.ID != "" {
		t.Errorf("running order = %+v", g.Race)
	}
}
