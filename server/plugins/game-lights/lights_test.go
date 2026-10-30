package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	var rp struct {
		Team  Team   `json:"team"`
		Plays []Play `json:"plays"`
	}
	if err := json.Unmarshal(replay, &rp); err != nil {
		t.Fatal(err)
	}
	want := []string{"", "touchdown good mine", "interception bad", "sack bad", "", "turnover_on_downs bad", "field_goal good mine", "touchdown bad"}
	for i, p := range rp.Plays {
		e, ok := classify(p, rp.Team.ID)
		got := ""
		if ok {
			got = e.Kind + map[bool]string{true: " good", false: " bad"}[e.Good] + map[bool]string{true: " mine", false: ""}[e.Mine]
		}
		if got != want[i] {
			t.Errorf("play %d (%s): %q, want %q", i+1, p.Text, got, want[i])
		}
	}
	// The core API names teams by $ref.
	var core Play
	json.Unmarshal([]byte(`{"type":{"text":"Pass Interception Return"},"text":"intercepted",
		"team":{"$ref":"http://sports.core.api.espn.com/v2/sports/football/leagues/college-football/seasons/2026/teams/999?lang=en"},
		"start":{"team":{"$ref":"http://sports.core.api.espn.com/v2/sports/football/leagues/college-football/seasons/2026/teams/259?lang=en"}}}`), &core)
	if e, ok := classify(core, "259"); !ok || e.Kind != "interception" || e.Good {
		t.Errorf("core API play: %+v %v", e, ok)
	}
	for e, key := range map[Event]string{
		{Kind: "interception", Good: true}:      "takeaway",
		{Kind: "safety", Mine: true}:            "takeaway",
		{Kind: "safety"}:                        "opp_score",
		{Kind: "sack", Good: true}:              "sack_for",
		{Kind: "sack"}:                          "sack_against",
		{Kind: "goal", Mine: true}:              "touchdown",
		{Kind: "run", Mine: true}:               "field_goal",
		{Kind: "run"}:                           "opp_score",
		{Kind: "win", Good: true}:               "win",
		{Kind: "fumble_lost", Good: false}:      "turnover_against",
		{Kind: "turnover_on_downs", Good: true}: "takeaway",
	} {
		if got := effectKey(e); got != key {
			t.Errorf("%+v → %q, want %q", e, got, key)
		}
	}
}

func TestColours(t *testing.T) {
	if got := teamHex("VT", "630031", "cf4420", nil); got != [2]string{"#861F41", "#E5751F"} {
		t.Errorf("VT override: %v", got)
	}
	if got := teamHex("ARMY", "000000", "d4bf91", nil); got != [2]string{"#d4bf91", "#d4bf91"} {
		t.Errorf("black swapped out: %v", got)
	}
	if got := teamHex("TEX", "bf5700", "ffffff", nil); got != [2]string{"#bf5700", "#bf5700"} {
		t.Errorf("white swapped out: %v", got)
	}
	custom := [2]string{"#112233", "#445566"}
	if got := teamHex("VT", "", "", &custom); got != custom {
		t.Errorf("admin's colours: %v", got)
	}
	vt := palette(teamHex("VT", "", "", nil))
	if math.Abs(vt.Primary.X-0.56) > 0.03 || vt.Primary.Bri < 89 {
		t.Errorf("VT maroon: %+v", vt.Primary)
	}
	// One colour: the second step is the same hue at another brightness, so it still flashes.
	tex := palette(teamHex("TEX", "bf5700", "ffffff", nil))
	if tex.Secondary.X != tex.Primary.X || tex.Secondary.Bri == tex.Primary.Bri {
		t.Errorf("single-colour team: %+v", tex)
	}
	for _, c := range []Colour{hexColour("#00ff00"), hexColour("#0000ff"), red, amber} {
		if c.X <= 0 || c.X > 0.75 || c.Y <= 0 || c.Y > 0.85 || c.Bri < 1 || c.Bri > 254 {
			t.Errorf("out of range: %+v", c)
		}
	}
	for _, e := range Effects {
		steps := effectSteps(e.Key, vt, false)
		if len(steps) == 0 {
			t.Errorf("%s has no steps", e.Key)
		}
		for _, s := range steps {
			if s.Hold < 350*time.Millisecond {
				t.Errorf("%s flashes faster than 3 a second", e.Key)
			}
		}
		for _, s := range effectSteps(e.Key, vt, true) {
			if s.Hold < 500*time.Millisecond {
				t.Errorf("%s with reduced flashing: %v", e.Key, s.Hold)
			}
		}
	}
	td := effectSteps("touchdown", vt, false)
	if td[0].Colour == td[1].Colour {
		t.Error("touchdown doesn't alternate")
	}
}

// fakeBridge records light changes.
type fakeBridge struct {
	mu     sync.Mutex
	lights map[string]LightState
	sets   []string
}

func (b *fakeBridge) Lights(context.Context) ([]Light, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Light
	for id, s := range b.lights {
		out = append(out, Light{ID: id, Name: "Lamp " + id, State: s, Colour: true})
	}
	return out, nil
}

func (b *fakeBridge) SetState(_ context.Context, id string, st map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.lights[id]
	s.On = st["on"].(bool)
	s.Bri = st["bri"].(int)
	if xy, ok := st["xy"].([]float64); ok {
		s.XY, s.ColorMode = xy, "xy"
	}
	b.lights[id] = s
	b.sets = append(b.sets, fmt.Sprintf("%s %.2f", id, s.XY[0]))
	return nil
}

func (b *fakeBridge) Areas(context.Context) ([]Area, error) {
	return []Area{{ID: "10", Name: "Living room", Type: "Room", Lights: []string{"1", "2"}}, {ID: "11", Name: "Den", Type: "Room", Lights: []string{"3"}}}, nil
}

func (b *fakeBridge) Restore(_ context.Context, id string, s LightState) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lights[id] = s
	return nil
}

func testApp(t *testing.T) (*app, *fakeBridge) {
	a := newApp(func() string { return t.TempDir() })
	dir := t.TempDir()
	a.conf.dir = func() string { return dir }
	fb := &fakeBridge{lights: map[string]LightState{
		"1": {On: true, Bri: 120, XY: []float64{0.45, 0.41}, ColorMode: "xy"},
		"2": {On: false, Bri: 200, XY: []float64{0.3, 0.3}, ColorMode: "xy"},
		"3": {On: true, Bri: 50, XY: []float64{0.2, 0.2}, ColorMode: "xy"}, // not chosen
	}}
	a.runner.bridge = func(Config) bridgeAPI { return fb }
	a.runner.gap = time.Millisecond
	a.settings = map[string]string{"delay_seconds": "0", "brightness": "100", "reduced_flashing": "on"}
	// The sports events in these tests come from user 0, who turned Game Lights on.
	a.conf.set(Config{Bridge: "bridge", Username: "user", Lights: []string{"1", "2"}, Version: 2, People: map[string]Person{"0": {Enabled: true}}})
	return a, fb
}

func wait(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 1000 && !cond(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !cond() {
		t.Fatal("timed out")
	}
}

func TestRunnerRestores(t *testing.T) {
	a, fb := testApp(t)
	before := map[string]LightState{}
	for k, v := range fb.lights {
		before[k] = v
	}
	a.runner.Fire("sack_against", palette([2]string{"#861F41", "#E5751F"}), "test", 0)
	wait(t, func() bool { return a.runner.Idle() })
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.sets) == 0 {
		t.Fatal("no light changes")
	}
	for _, s := range fb.sets {
		if strings.HasPrefix(s, "3 ") {
			t.Error("touched a light that wasn't chosen")
		}
	}
	for id, want := range before {
		got := fb.lights[id]
		if got.On != want.On || got.Bri != want.Bri || got.XY[0] != want.XY[0] {
			t.Errorf("light %s: %+v, want %+v", id, got, want)
		}
	}
}

// sportsData builds a sports.* event body.
func sportsData(state, scorer, homeScore, awayScore string, homeWins bool, sport, league string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"sport": sport, "scorer": scorer,
		"game": map[string]any{"id": "401", "league": league, "shortName": "MIA @ VT", "state": state, "detail": "Final",
			"home": map[string]any{"id": "259", "name": "Virginia Tech Hokies", "shortName": "Hokies", "abbr": "VT", "color": "630031", "altColor": "cf4420", "score": homeScore, "winner": homeWins},
			"away": map[string]any{"id": "2390", "name": "Miami Hurricanes", "shortName": "Hurricanes", "abbr": "MIA", "color": "005030", "altColor": "f47321", "score": awayScore}},
		"fans": []map[string]any{{"user": "michael", "teamId": "259"}},
	})
	return b
}

func TestFollowsScores(t *testing.T) {
	a, fb := testApp(t)
	ctx := context.Background()
	// Hockey: no play feed, each goal flashes.
	a.follower.onEvent(ctx, "sports.game.live", sportsData("in", "", "0", "0", false, "hockey", "nhl"))
	a.follower.onEvent(ctx, "sports.score", sportsData("in", "259", "1", "0", false, "hockey", "nhl"))
	wait(t, func() bool {
		fb.mu.Lock()
		defer fb.mu.Unlock()
		return len(fb.sets) > 0
	})
	a.runner.Reset()
	wait(t, func() bool { return a.runner.Idle() })
	if games := a.follower.Games(); len(games) != 1 || !strings.Contains(games[0], "Hokies") {
		t.Errorf("games = %v", games)
	}
	if _, ok := a.conf.get().Teams["nhl/259"]; !ok {
		t.Error("the team wasn't remembered for the colours list")
	}
	// Someone else's picks are ignored when the setting names people.
	a.settings["users"] = "dana"
	a.follower.drop("401")
	a.follower.onEvent(ctx, "sports.game.live", sportsData("in", "", "1", "0", false, "hockey", "nhl"))
	if len(a.follower.Games()) != 0 {
		t.Error("followed a team nobody named picked")
	}
	a.settings["users"] = "Michael"
	a.follower.onEvent(ctx, "sports.game.end", sportsData("post", "", "3", "1", true, "hockey", "nhl"))
	if len(a.follower.Games()) != 0 {
		t.Error("still following after the end")
	}
	wait(t, func() bool { return logged(a, "Lights: Hokies won") })
	a.runner.Reset()
	wait(t, func() bool { return a.runner.Idle() })
}

func logged(a *app, text string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, l := range a.log {
		if strings.Contains(l.Text, text) {
			return true
		}
	}
	return false
}

func TestFootballPlays(t *testing.T) {
	var mu sync.Mutex
	plays := []string{`{"id":"1","type":{"text":"Rush"},"text":"old play","start":{"team":{"id":"259"}},"statYardage":40}`}
	espn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/football/leagues/college-football/events/401/competitions/401/plays") {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(plays, ","))
	}))
	defer espn.Close()
	old := espnCore
	espnCore = espn.URL
	defer func() { espnCore = old }()

	a, _ := testApp(t)
	a.follower.every = 20 * time.Millisecond
	ctx := context.Background()
	a.follower.onEvent(ctx, "sports.game.start", sportsData("in", "", "0", "0", false, "football", "college-football"))
	defer a.follower.drop("401")
	wait(t, func() bool {
		a.follower.mu.Lock()
		defer a.follower.mu.Unlock()
		return a.follower.games["401"].playsOK
	})
	mu.Lock()
	plays = append(plays, `{"id":"2","type":{"text":"Passing Touchdown"},"text":"pass for a TOUCHDOWN","scoringPlay":true,"team":{"id":"259"},"start":{"team":{"id":"259"}}}`)
	mu.Unlock()
	// The score change for the same touchdown is left to the plays.
	a.follower.onEvent(ctx, "sports.score", sportsData("in", "259", "7", "0", false, "football", "college-football"))
	wait(t, func() bool { return logged(a, "TOUCHDOWN") })
	a.runner.Reset()
	wait(t, func() bool { return a.runner.Idle() })
	a.mu.Lock()
	defer a.mu.Unlock()
	lights := 0
	for _, l := range a.log {
		if strings.HasPrefix(l.Text, "Lights: ") {
			lights++
		}
		if strings.Contains(l.Text, "old play") || strings.Contains(l.Text, "MIA @ VT 0–7") {
			t.Errorf("replayed history or doubled the score: %q", l.Text)
		}
	}
	if lights > 1 {
		t.Errorf("%d effects for one touchdown", lights)
	}
}

// A fresh, unpaired plugin has no activity yet; the page reads every list's
// length, so none of them may go out as null.
func TestStatusListsNotNull(t *testing.T) {
	a := newApp(func() string { return t.TempDir() })
	dir := t.TempDir()
	a.conf.dir = func() string { return dir }
	rec := httptest.NewRecorder()
	a.status(rec, httptest.NewRequest("GET", "/status", nil))
	var got map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"games", "log", "effects", "teams"} {
		if s := string(got[k]); s == "" || s == "null" {
			t.Errorf("%s = %q, want a list", k, s)
		}
	}
}

// A person who never saved anything still gets delays and colours as {}:
// the TV app and the web page index into them, and crashed on a missing one.
func TestMyStateMapsNotNull(t *testing.T) {
	a := newApp(func() string { return t.TempDir() })
	dir := t.TempDir()
	a.conf.dir = func() string { return dir }
	rec := httptest.NewRecorder()
	a.myState(rec, httptest.NewRequest("GET", "/me/state", nil), "7")
	var got struct {
		Person map[string]json.RawMessage `json:"person"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"delays", "colours", "source"} {
		if s := string(got.Person[k]); s == "" || s == "null" {
			t.Errorf("person.%s = %q, want it set", k, s)
		}
	}
}

// Two people in one game, cheering for different teams in different rooms,
// each with their own delay source and choices.
func TestPeopleRoomsAndDelays(t *testing.T) {
	a, fb := testApp(t)
	a.conf.update(func(c *Config) {
		c.People = map[string]Person{
			"1": {Enabled: true, Area: "10", Source: "YouTube TV", Delays: map[string]int{"YouTube TV": 0, "Cable or antenna": 60}, Opponent: "colour"},
			"2": {Enabled: true, Area: "11", Opponent: "off"},
		}
	})
	a.settings["delay_seconds"] = "0"
	b, _ := json.Marshal(map[string]any{
		"sport": "hockey", "scorer": "2390",
		"game": map[string]any{"id": "401", "league": "nhl", "shortName": "MIA @ VT", "state": "in",
			"home": map[string]any{"id": "259", "shortName": "Hokies", "abbr": "VT", "color": "630031", "altColor": "cf4420", "score": "0"},
			"away": map[string]any{"id": "2390", "shortName": "Hurricanes", "abbr": "MIA", "color": "005030", "altColor": "f47321", "score": "1"}},
		"fans": []map[string]any{{"userId": 1, "user": "michael", "teamId": "259"}, {"userId": 2, "user": "dana", "teamId": "2390"}},
	})
	a.follower.onEvent(context.Background(), "sports.score", b)
	// Michael (living room) sees Miami's goal as a dim pulse in Miami's
	// colours; Dana (den) gets their team's touchdown effect.
	wait(t, func() bool {
		return logged(a, "Lights: MIA @ VT 1–0") && a.runnerFor("10").Idle() && a.runnerFor("11").Idle()
	})
	fb.mu.Lock()
	rooms := map[string]bool{}
	for _, s := range fb.sets {
		rooms[s[:1]] = true
	}
	fb.mu.Unlock()
	if !rooms["1"] || !rooms["3"] {
		t.Errorf("lights changed: %v, want the living room (1, 2) and the den (3)", rooms)
	}
	if at, text := a.follower.Last("1"); at.IsZero() || !strings.Contains(text, "1–0") {
		t.Errorf("last play for calibrating = %q", text)
	}

	// Calibrating sets the delay for the current source only.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/me/calibrate", nil)
	req.Header.Set("X-BingeBandit-User-Id", "1")
	a.routes().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("calibrate: %d %s", rec.Code, rec.Body)
	}
	p := a.conf.get().People["1"]
	if p.Delays["Cable or antenna"] != 60 || p.Area != "10" {
		t.Errorf("calibrating changed other choices: %+v", p)
	}
	if d := p.delay(25); d > 20 || d == 25 {
		t.Errorf("calibrated delay = %d", p.delay(25))
	}

	// Saving checks what it's given.
	for body, code := range map[string]int{`{"opponent":"rainbow"}`: 400, `{"delays":{"Cable":999}}`: 400, `{"area":"11","opponent":"red"}`: 204} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/me/settings", strings.NewReader(body))
		req.Header.Set("X-BingeBandit-User-Id", "2")
		a.routes().ServeHTTP(rec, req)
		if rec.Code != code {
			t.Errorf("%s: %d, want %d", body, rec.Code, code)
		}
	}
	a.resetAll()
}

// Game Lights is opt-in per person, and each team can be left out.
func TestOptInAndSkippedTeams(t *testing.T) {
	a, fb := testApp(t)
	sets := func() int {
		fb.mu.Lock()
		defer fb.mu.Unlock()
		return len(fb.sets)
	}
	score := func(n string) {
		a.follower.onEvent(context.Background(), "sports.score", sportsData("in", "259", n, "0", false, "hockey", "nhl"))
		time.Sleep(150 * time.Millisecond)
		wait(t, func() bool { return a.runner.Idle() })
	}
	for _, p := range []Person{{}, {Enabled: true, Off: true}, {Enabled: true, Skip: []string{"nhl/259"}}} {
		a.conf.update(func(c *Config) { c.People = map[string]Person{"0": p} })
		before := sets()
		score("1")
		if sets() != before {
			t.Errorf("%+v: the lights flashed", p)
		}
	}
	a.conf.update(func(c *Config) { c.People = map[string]Person{"0": {Enabled: true, Skip: []string{"nhl/2390"}}} })
	score("2")
	if sets() == 0 {
		t.Error("an enabled person's team didn't flash")
	}
	a.resetAll()
}

// People who had set their own choices before it was opt-in keep it on.
func TestConfigVersion2(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"people":{"1":{"area":"10"},"2":{"off":true}}}`), 0o600)
	s := &configStore{dir: func() string { return dir }}
	c := s.get()
	if !c.People["1"].Enabled || !c.People["2"].Enabled || !c.People["2"].Off || c.Version != 2 {
		t.Errorf("after upgrade: %+v", c)
	}
}

// Someone whose TV drives their own hub gets cues to poll, and the
// server's bridge stays still.
func TestLocalCues(t *testing.T) {
	a, fb := testApp(t)
	a.conf.update(func(c *Config) {
		c.People = map[string]Person{"1": {Enabled: true, Local: true}}
	})
	a.settings["delay_seconds"] = "0"
	get := func(after string) (out struct {
		Cues []struct {
			ID    int64     `json:"id"`
			InMs  int64     `json:"inMs"`
			Steps []cueStep `json:"steps"`
		} `json:"cues"`
		Last int64 `json:"last"`
	}) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/me/cues?after="+after, nil)
		req.Header.Set("X-BingeBandit-User-Id", "1")
		a.routes().ServeHTTP(rec, req)
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	if s := get(""); len(s.Cues) != 0 || s.Last != 0 {
		t.Fatalf("first poll = %+v, want only where to start", s)
	}
	b, _ := json.Marshal(map[string]any{
		"sport": "hockey", "scorer": "259",
		"game": map[string]any{"id": "401", "league": "nhl", "shortName": "MIA @ VT", "state": "in",
			"home": map[string]any{"id": "259", "shortName": "Hokies", "abbr": "VT", "color": "630031", "altColor": "cf4420", "score": "1"},
			"away": map[string]any{"id": "2390", "shortName": "Hurricanes", "abbr": "MIA", "color": "005030", "altColor": "f47321", "score": "0"}},
		"fans": []map[string]any{{"userId": 1, "user": "michael", "teamId": "259"}},
	})
	a.follower.onEvent(context.Background(), "sports.score", b)
	s := get("0")
	if len(s.Cues) != 1 || len(s.Cues[0].Steps) == 0 || s.Last != s.Cues[0].ID {
		t.Fatalf("cues = %+v", s)
	}
	if again := get(strconv.FormatInt(s.Last, 10)); len(again.Cues) != 0 {
		t.Errorf("a cue came twice: %+v", again)
	}
	time.Sleep(200 * time.Millisecond)
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if len(fb.sets) != 0 {
		t.Errorf("the server's bridge changed lights for a TV-driven person: %v", fb.sets)
	}
}
