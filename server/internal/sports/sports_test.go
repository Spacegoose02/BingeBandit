package sports

import (
	"context"
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
