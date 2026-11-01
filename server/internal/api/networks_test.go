package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/sports"
	"bingebandit/internal/store"
)

// The Sports page finds a CBS game on the local CBS station an admin mapped,
// not on another city's CBS whose guide happens to name the teams.
func TestNetworkChannels(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("dana", admin.ID, "Web", "")
	st.CreateSession("sam", sam.ID, "Web", "")

	src := &store.LiveSource{Name: "Guide", Kind: "epg", URL: "http://x"}
	if err := st.SaveLiveSource(src); err != nil {
		t.Fatal(err)
	}
	wcbs := &store.Channel{Number: 2, Name: "WCBS New York", EPGID: "wcbs.us", Enabled: true}
	wdbj := &store.Channel{Number: 7, Name: "WDBJ", EPGID: "wdbj.us", Enabled: true}
	wsls := &store.Channel{Number: 10, Name: "WSLS", EPGID: "wsls.us", Enabled: true}
	bare := &store.Channel{Number: 13, Name: "WSET", Enabled: true} // no guide
	for _, c := range []*store.Channel{wcbs, wdbj, wsls, bare} {
		if err := st.CreateChannel(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	kick := time.Now().Add(2 * time.Hour).Truncate(time.Hour)
	gi, err := st.BeginGuideImport(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"wcbs.us", "wdbj.us", "wsls.us"} {
		gi.Channel(id, id, "")
	}
	prog := func(epg, title, desc, cat string) {
		p := store.GuideProgram{EPGID: epg, Start: kick.Add(-5 * time.Minute).Unix(), Stop: kick.Add(3 * time.Hour).Unix(), Title: title, Description: desc, Category: cat}
		if err := gi.Program(&p); err != nil {
			t.Fatal(err)
		}
	}
	prog("wcbs.us", "College Football", "Miami at Virginia Tech.", "Sports")
	prog("wdbj.us", "SEC on CBS", "", "")                                 // no teams named
	prog("wsls.us", "College Football: Alabama at Georgia", "", "Sports") // another region's game
	if _, err := gi.Finish(); err != nil {
		t.Fatal(err)
	}

	srv := &Server{Store: st}
	game := func(networks ...string) []sportsGame {
		return []sportsGame{{Game: sports.Game{ID: "401", League: "college-football", State: "pre", Start: kick, Broadcasts: networks,
			Home: sports.Side{Team: sports.Team{ID: "259", Name: "Virginia Tech Hokies", Short: "Hokies"}},
			Away: sports.Side{Team: sports.Team{ID: "2390", Name: "Miami Hurricanes", Short: "Hurricanes"}}}}}
	}
	names := func(g []sportsGame) []string {
		var out []string
		for _, c := range g[0].Channels {
			out = append(out, c.Name)
		}
		return out
	}
	find := func(networks ...string) []string {
		g := game(networks...)
		srv.findChannels(admin.ID, g)
		return names(g)
	}

	// Unmapped: only the guide titles, which point at the wrong city.
	if got := find("CBS"); !slices.Equal(got, []string{"WCBS New York"}) {
		t.Errorf("unmapped: %v", got)
	}

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
	body, _ := json.Marshal([]store.NetworkChannels{
		{Network: " cbs ", Channels: []int64{wdbj.ID, 99999}},
		{Network: "NBC", Channels: []int64{wsls.ID}},
		{Network: "ABC", Channels: []int64{bare.ID}},
		{Network: "", Channels: []int64{wcbs.ID}},
	})
	if code := call("sam", "PUT", "/api/admin/live/networks", string(body), nil); code != 403 {
		t.Errorf("non-admin save: %d", code)
	}
	var out networksOut
	if code := call("dana", "PUT", "/api/admin/live/networks", string(body), &out); code != 200 || len(out.Networks) != 3 ||
		out.Networks[1].Network != "cbs" || !slices.Equal(out.Networks[1].Channels, []int64{wdbj.ID}) {
		t.Fatalf("save: %d %+v", code, out)
	}
	if !slices.Contains(out.Known, "CBS") || !slices.Contains(out.Known, "ESPN") {
		t.Errorf("known networks: %v", out.Known)
	}

	// CBS → WDBJ: its generic sports title counts, and other cities' CBS
	// stations drop out.
	if got := find("CBS", "Paramount+"); !slices.Equal(got, []string{"WDBJ"}) {
		t.Errorf("mapped CBS: %v", got)
	}
	// NBC → WSLS, but WSLS shows another region's game: back to the guide.
	if got := find("NBC"); !slices.Equal(got, []string{"WCBS New York"}) {
		t.Errorf("regional NBC: %v", got)
	}
	// A channel with no guide is taken on trust.
	if got := find("ABC"); !slices.Equal(got, []string{"WSET"}) {
		t.Errorf("no guide: %v", got)
	}
	// ESPN listed this game's networks, so the admin page offers them.
	find("Paramount+")
	var again networksOut
	if code := call("dana", "GET", "/api/admin/live/networks", "", &again); code != 200 || !slices.Contains(again.Known, "Paramount+") {
		t.Errorf("seen networks: %d %v", code, again.Known)
	}

	// A deleted channel leaves the mapping.
	if err := st.DeleteChannel(wdbj.ID); err != nil {
		t.Fatal(err)
	}
	list, _ := st.Networks()
	if len(list) != 2 || list[0].Network != "ABC" || list[1].Network != "NBC" {
		t.Errorf("after delete: %+v", list)
	}
}

func TestCarriesGame(t *testing.T) {
	kick := time.Now().Truncate(time.Hour)
	g := &sportsGame{Game: sports.Game{Start: kick,
		Home: sports.Side{Team: sports.Team{ID: "1", Name: "Washington Commanders", Short: "Commanders"}},
		Away: sports.Side{Team: sports.Team{ID: "2", Name: "New York Giants", Short: "Giants"}}}}
	c := &store.Channel{EPGID: "x"}
	for _, tc := range []struct {
		title, sub, cat string
		want            bool
	}{
		{"NFL Football", "", "", true},
		{"The NFL Today", "", "", true},
		{"NFL Football", "Giants at Commanders", "", true},
		{"NFL Football", "Bears at Packers", "", false},
		{"Bears vs. Packers", "", "Sports", false},
		{"60 Minutes", "", "News", false},
		{"Local Weekend", "", "Sports event", true},
	} {
		p := store.GuideProgram{EPGID: "x", Start: kick.Unix(), Stop: kick.Add(time.Hour).Unix(), Title: tc.title, Subtitle: tc.sub, Category: tc.cat}
		if _, ok := carriesGame(c, g, []store.GuideProgram{p}); ok != tc.want {
			t.Errorf("%q / %q: %v", tc.title, tc.sub, ok)
		}
	}
	if _, ok := carriesGame(c, g, nil); !ok {
		t.Error("no guide entry at kick-off should count")
	}
}
