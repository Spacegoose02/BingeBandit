package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/sports"
	"bingebandit/internal/store"
)

func TestSports(t *testing.T) {
	start := time.Now().Add(10 * time.Minute).Truncate(time.Minute)
	var mu sync.Mutex
	state, home, away := "pre", "0", "0"
	espn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		team := func(id, name, short, abbr string) string {
			return fmt.Sprintf(`{"id":%q,"displayName":%q,"shortDisplayName":%q,"abbreviation":%q,"color":"630031","alternateColor":"cf4420","logo":"https://a.espncdn.com/%s.png"}`, id, name, short, abbr, abbr)
		}
		vt, mia := team("259", "Virginia Tech Hokies", "Hokies", "VT"), team("2390", "Miami Hurricanes", "Hurricanes", "MIA")
		switch {
		case strings.HasSuffix(r.URL.Path, "/college-football/teams"):
			fmt.Fprintf(w, `{"sports":[{"leagues":[{"teams":[{"team":%s},{"team":%s}]}]}]}`, vt, mia)
		case strings.HasSuffix(r.URL.Path, "/college-football/scoreboard") && r.URL.Query().Get("dates") == sports.Day(start):
			fmt.Fprintf(w, `{"events":[{"id":"401","date":%q,"name":"Miami Hurricanes at Virginia Tech Hokies","shortName":"MIA @ VT",
				"competitions":[{"broadcasts":[{"names":["ACC Network"]}],"competitors":[
					{"homeAway":"home","score":%q,"winner":%v,"team":%s},{"homeAway":"away","score":%q,"team":%s}]}],
				"status":{"type":{"state":%q,"detail":"Final"}}}]}`,
				start.UTC().Format("2006-01-02T15:04Z"), home, state == "post", vt, away, mia, state)
		case strings.HasSuffix(r.URL.Path, "/scoreboard"):
			fmt.Fprint(w, `{"events":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer espn.Close()
	old := sports.BaseURL
	sports.BaseURL = espn.URL
	defer func() { sports.BaseURL = old }()

	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("dana", dana.ID, "Web", "")
	st.CreateSession("sam", sam.ID, "Web", "")
	p, _ := st.UserPrefs(sam.ID)
	p.GameAlertMinutes, p.NotifyGameScores = 0, true
	st.SetUserPrefs(sam.ID, p)

	srv := &Server{Store: st}
	h := srv.Router(context.Background())
	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}

	var teams []sports.Team
	if code := call("dana", "GET", "/api/sports/college-football/teams", "", &teams); code != 200 || len(teams) != 2 {
		t.Fatalf("teams: %d %+v", code, teams)
	}
	if code := call("dana", "GET", "/api/sports/cricket/teams", "", nil); code != 404 {
		t.Errorf("unknown league: %d", code)
	}
	if code := call("dana", "PUT", "/api/sports/teams", `[{"league":"college-football","id":"999"}]`, nil); code != 400 {
		t.Errorf("unknown team: %d", code)
	}
	for _, tok := range []string{"dana", "sam"} {
		if code := call(tok, "PUT", "/api/sports/teams", `[{"league":"college-football","id":"259"}]`, &teams); code != 200 || teams[0].Logo != "https://a.espncdn.com/VT.png" {
			t.Fatalf("pick: %d %+v", code, teams)
		}
	}

	var page struct {
		Leagues []sports.League `json:"leagues"`
		Teams   []sports.Team   `json:"teams"`
		Games   []sportsGame    `json:"games"`
	}
	if code := call("dana", "GET", "/api/sports", "", &page); code != 200 || len(page.Teams) != 1 || len(page.Games) != 1 || page.Games[0].Mine[0] != "259" || len(page.Leagues) < 8 {
		t.Fatalf("sports page: %d %+v", code, page)
	}
	var day []sports.Game
	if code := call("dana", "GET", "/api/sports/college-football/scores?date="+sports.Day(start), "", &day); code != 200 || len(day) != 1 {
		t.Fatalf("scores: %d %+v", code, day)
	}

	// The score bug's list: only games that are on.
	var live struct {
		Games []sportsGame `json:"games"`
	}
	if code := call("dana", "GET", "/api/sports/live", "", &live); code != 200 || len(live.Games) != 0 {
		t.Fatalf("live before kick-off: %d %+v", code, live)
	}
	if sports.Day(start) == sports.Day(time.Now()) { // not just before midnight
		mu.Lock()
		state, home = "in", "7"
		mu.Unlock()
		srv.Sports = sports.NewClient()
		if code := call("dana", "GET", "/api/sports/live", "", &live); code != 200 || len(live.Games) != 1 || live.Games[0].Home.Score != "7" {
			t.Fatalf("live: %d %+v", code, live)
		}
		mu.Lock()
		state, home = "pre", "0"
		mu.Unlock()
	}

	// The tracker: Dana gets the "starting soon" alert (15 minutes is the
	// default), Sam turned it off but wants final scores.
	srv.Sports.Now = time.Now
	tr := &sports.Tracker{Client: srv.Sports, Follows: srv.sportsFollows, On: srv.onSports}
	look := func(s, h, a string) {
		mu.Lock()
		state, home, away = s, h, a
		mu.Unlock()
		srv.Sports = sports.NewClient() // no cache between looks
		tr.Client = srv.Sports
		tr.Look(context.Background())
	}
	look("pre", "0", "0")
	look("pre", "0", "0")
	look("in", "7", "0")
	look("post", "24", "21")
	notes := func(uid int64) []*store.Notification {
		list, _, _ := st.Notifications(uid, 10)
		return list
	}
	d := notes(dana.ID)
	if len(d) != 1 || d[0].Kind != "sports" || !strings.HasPrefix(d[0].Title, "Hokies vs Hurricanes starts in ") || d[0].Body != "ACC Network" || d[0].Image != "https://a.espncdn.com/VT.png" {
		t.Errorf("dana's notifications: %+v", d)
	}
	sn := notes(sam.ID)
	if len(sn) != 1 || sn[0].Title != "Hokies beat Hurricanes 24–21" {
		for _, n := range sn {
			t.Logf("%+v", n)
		}
		t.Errorf("sam's notifications: %d", len(sn))
	}
	// A second final look (say, after a restart) doesn't send it again.
	look("post", "24", "21")
	if n := notes(sam.ID); len(n) != 1 {
		t.Errorf("final sent twice: %d", len(n))
	}
}

func TestNamesTeam(t *testing.T) {
	vt := sports.Team{Name: "Virginia Tech Hokies", Short: "Hokies"}
	mia := sports.Team{Name: "Miami Dolphins", Short: "Dolphins"}
	for text, want := range map[string]bool{
		"College Football: Miami at Virginia Tech": true,
		"Hokies Football Live":                     true,
		"West Virginia Technical Highlights":       false,
		"NFL: Miami Dolphins at Buffalo Bills":     true,
	} {
		if got := namesTeam(text, vt) || namesTeam(text, mia) && strings.Contains(text, "Dolphins"); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
	if namesTeam("Miamisburg High", mia) {
		t.Error("matched part of a word")
	}
}

// The score bug hides while someone watches the game, even when the guide
// calls it "College Football" and names only one team.
func TestScoreBugOnChannel(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, _ := st.CreateUser("fan", "x", true)
	src := &store.LiveSource{Name: "Guide", Kind: "epg", URL: "http://x"}
	if err := st.SaveLiveSource(src); err != nil {
		t.Fatal(err)
	}
	acc := &store.Channel{Number: 40, Name: "ACC Network", EPGID: "acc.us", Enabled: true}
	news := &store.Channel{Number: 4, Name: "ABC", EPGID: "abc.us", Enabled: true}
	for _, c := range []*store.Channel{acc, news} {
		if err := st.CreateChannel(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().Truncate(time.Minute)
	gi, err := st.BeginGuideImport(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	gi.Channel("acc.us", "ACC Network", "")
	gi.Channel("abc.us", "ABC", "")
	for _, pr := range []store.GuideProgram{
		{EPGID: "acc.us", Start: now.Add(-time.Hour).Unix(), Stop: now.Add(2 * time.Hour).Unix(), Title: "College Football", Description: "The Hokies host their rivals."},
		{EPGID: "abc.us", Start: now.Add(-time.Hour).Unix(), Stop: now.Add(2 * time.Hour).Unix(), Title: "The News"},
	} {
		if err := gi.Program(&pr); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gi.Finish(); err != nil {
		t.Fatal(err)
	}
	game := func() []sportsGame {
		return []sportsGame{{Game: sports.Game{ID: "401", State: "in", Start: now.Add(-time.Hour),
			Home: sports.Side{Team: sports.Team{ID: "259", Name: "Virginia Tech Hokies", Short: "Hokies"}},
			Away: sports.Side{Team: sports.Team{ID: "2390", Name: "Miami Hurricanes", Short: "Hurricanes"}}}}}
	}
	s := &Server{Store: st}
	g := game()
	s.onChannel(u.ID, acc.ID, g)
	if len(g[0].Channels) != 1 || g[0].Channels[0].ID != acc.ID {
		t.Errorf("watching the game: %+v", g[0].Channels)
	}
	g = game()
	s.onChannel(u.ID, news.ID, g)
	if len(g[0].Channels) != 0 {
		t.Errorf("watching the news: %+v", g[0].Channels)
	}
}
