package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bingebandit/pkg/bbplugin"
)

func TestColours(t *testing.T) {
	rx, ry := rgbToXY(1, 0, 0)
	if math.Hypot(rx-0.64, ry-0.33) > 0.06 {
		t.Errorf("red: %.3f,%.3f", rx, ry)
	}
	bx, by := rgbToXY(0, 0, 1)
	if !inTriangle(bx, by) {
		t.Errorf("blue %.3f,%.3f outside the gamut", bx, by)
	}
	// Left half red, right half blue.
	frame := make([]byte, frameW*frameH*3)
	for y := 0; y < frameH; y++ {
		for x := 0; x < frameW; x++ {
			i := (y*frameW + x) * 3
			if x < frameW/2 {
				frame[i] = 255
			} else {
				frame[i+2] = 255
			}
		}
	}
	z := zoneColours(frame)
	if l, r := z["left"], z["right"]; l.X < 0.5 || r.X > 0.25 {
		t.Errorf("left %+v right %+v", l, r)
	}
	if b := hueBri(0, 100); b < 1 || b > 40 {
		t.Errorf("dark scenes should be dim, not off: %d", b)
	}
	if hueBri(1, 50) != 127 {
		t.Errorf("brightness cap: %d", hueBri(1, 50))
	}
}

// testVideo makes 6 s of red, green, then blue.
func testVideo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "rgb.mkv")
	out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "color=red:s=320x180:d=2:r=25",
		"-f", "lavfi", "-i", "color=lime:s=320x180:d=2:r=25", "-f", "lavfi", "-i", "color=blue:s=320x180:d=2:r=25",
		"-filter_complex", "[0][1][2]concat=n=3:v=1", "-c:v", "libx264", "-g", "25", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	return path
}

func near(c Colour, x, y float64) bool { return math.Hypot(c.X-x, c.Y-y) < 0.08 }

var (
	redXY   = [2]float64{0.64, 0.33}
	greenXY = [2]float64{0.30, 0.60}
	blueXY  = [2]float64{0.155, 0.06}
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSampler(t *testing.T) {
	video := testVideo(t)
	s := startSampler(context.Background(), "ffmpeg", video, "", 0, 4, 60)
	defer s.Stop()
	for _, c := range []struct {
		at   float64
		want [2]float64
	}{{0.5, redXY}, {2.5, greenXY}, {4.5, blueXY}} {
		waitFor(t, "frames", func() bool { _, ok := s.At(c.at); return ok })
		z, _ := s.At(c.at)
		if !near(z["all"], c.want[0], c.want[1]) {
			t.Errorf("at %.1fs: %+v, want near %v", c.at, z["all"], c.want)
		}
	}
	// Started part-way in, frame 0 is that time.
	s2 := startSampler(context.Background(), "ffmpeg", video, "", 4, 4, 60)
	defer s2.Stop()
	waitFor(t, "frames from 4 s", func() bool { _, ok := s2.At(4.5); return ok })
	if z, _ := s2.At(4.5); !near(z["all"], blueXY[0], blueXY[1]) {
		t.Errorf("from 4 s: %+v", z["all"])
	}
	if _, ok := s2.At(1); ok || s2.Covers(1) {
		t.Error("a sampler started at 4 s has nothing for 1 s")
	}
}

// fakeBridge records light changes.
type fakeBridge struct {
	mu     sync.Mutex
	states map[string]map[string]any
	puts   []string
	lights string // JSON for /lights and /groups; empty for the defaults
	groups string
}

func (f *fakeBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "POST" && r.URL.Path == "/api":
		io.WriteString(w, `[{"success":{"username":"u1"}}]`)
	case r.Method == "GET" && r.URL.Path == "/api/u1/lights" && f.lights != "":
		io.WriteString(w, f.lights)
	case r.Method == "GET" && r.URL.Path == "/api/u1/groups" && f.groups != "":
		io.WriteString(w, f.groups)
	case r.Method == "GET" && r.URL.Path == "/api/u1/lights":
		io.WriteString(w, `{"1":{"name":"Left lamp","type":"Extended color light","state":{"on":true,"bri":100,"xy":[0.45,0.41],"colormode":"xy"}},
			"2":{"name":"Hall","type":"Dimmable light","state":{"on":false,"bri":10}}}`)
	case r.Method == "GET" && r.URL.Path == "/api/u1/groups":
		io.WriteString(w, `{"1":{"name":"Living room","type":"Room","lights":["1","2"]}}`)
	case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/u1/lights/"):
		var st map[string]any
		json.NewDecoder(r.Body).Decode(&st)
		id := strings.Split(r.URL.Path, "/")[4]
		f.states[id] = st
		f.puts = append(f.puts, id)
		io.WriteString(w, `[{"success":{}}]`)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeBridge) xy(id string) (Colour, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	xy, ok := f.states[id]["xy"].([]any)
	if !ok {
		return Colour{}, false
	}
	return Colour{X: xy[0].(float64), Y: xy[1].(float64)}, true
}

func TestEngineFollowsPlayer(t *testing.T) {
	video := testVideo(t)
	bridge := &fakeBridge{states: map[string]map[string]any{}}
	hb := httptest.NewServer(bridge)
	defer hb.Close()

	a := &app{ffmpeg: "ffmpeg", p: &bbplugin.Plugin{Manifest: manifest}}
	a.settings = map[string]string{"updates_per_second": "10", "brightness": "100", "on_pause": "keep", "on_stop": "restore", "clients": "Web"}
	a.conf = &configStore{dir: t.TempDir}
	a.fileURL = func(int64) (string, string) { return video, "" }
	a.engine = &Engine{a: a}
	user, _, err := newBridge(hb.URL, "").Pair(context.Background())
	if err != nil || user != "u1" {
		t.Fatalf("pair: %q %v", user, err)
	}
	a.conf.set(Config{Bridge: hb.URL, Username: user, Lights: map[string]string{"1": "all"}})

	ctx := context.Background()
	off := false
	uid := int64(5)
	var lights *bool // nil: a server that predates the app switch
	send := func(event string, pos float64, client string) {
		d, _ := json.Marshal(playerEvent{SessionID: "s1", UserID: uid, User: "ann", Client: client, ItemID: 7, FileID: 3, Title: "RGB", Position: pos, Lights: lights})
		a.engine.onEvent(ctx, bbplugin.Event{Event: event, Data: d})
	}

	// Nobody has picked a mode yet: Hue Sync is opt-in.
	send("player.play", 0.2, "Web")
	if _, active := a.engine.Status(); active {
		t.Fatal("should not follow someone who hasn't turned Hue Sync on")
	}
	setMode := func(mode string) {
		req := httptest.NewRequest("PUT", "/me", strings.NewReader(`{"mode":"`+mode+`"}`))
		req.Header.Set("X-BingeBandit-User-Id", "5")
		rec := httptest.NewRecorder()
		a.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("set mode %s: %d %s", mode, rec.Code, rec.Body)
		}
	}
	setMode(ModeSync)

	// A device the filter leaves out is ignored.
	send("player.play", 0.2, "Android TV")
	if _, active := a.engine.Status(); active {
		t.Fatal("should only follow the Web client")
	}

	// Hue Sync switched off in the app: ignored too.
	lights = &off
	send("player.play", 0.2, "Web")
	if _, active := a.engine.Status(); active {
		t.Fatal("should not follow a device with Hue Sync off")
	}
	lights = nil

	send("player.play", 0.2, "Web")
	waitFor(t, "red", func() bool { c, ok := bridge.xy("1"); return ok && near(c, redXY[0], redXY[1]) })
	// The player reports it jumped to the blue part.
	send("player.progress", 4.4, "Web")
	waitFor(t, "blue", func() bool { c, ok := bridge.xy("1"); return ok && near(c, blueXY[0], blueXY[1]) })
	bridge.mu.Lock()
	for _, id := range bridge.puts {
		if id != "1" {
			t.Errorf("light %s isn't assigned and must not be touched", id)
		}
	}
	bridge.mu.Unlock()

	// Stopped: light 1 goes back to how it was.
	send("player.stop", 5, "Web")
	waitFor(t, "restore", func() bool { c, ok := bridge.xy("1"); return ok && math.Abs(c.X-0.45) < 0.001 })
	if _, active := a.engine.Status(); active {
		t.Error("should be idle after stop")
	}

	// Switched off mid-play: the lights let go at the next ping.
	on := true
	lights = &on
	send("player.play", 0.2, "Web")
	waitFor(t, "red again", func() bool { c, ok := bridge.xy("1"); return ok && near(c, redXY[0], redXY[1]) })
	lights = &off
	send("player.progress", 1, "Web")
	if _, active := a.engine.Status(); active {
		t.Error("should let go once Hue Sync is switched off")
	}
	waitFor(t, "restore again", func() bool { c, ok := bridge.xy("1"); return ok && math.Abs(c.X-0.45) < 0.001 })

	// Theater mode, picked mid-play from the player: the lights dim to a warm glow.
	lights = &on
	send("player.play", 0.2, "Web")
	waitFor(t, "red once more", func() bool { c, ok := bridge.xy("1"); return ok && near(c, redXY[0], redXY[1]) })
	setMode(ModeTheater)
	dimmed := func() bool {
		bridge.mu.Lock()
		defer bridge.mu.Unlock()
		st := bridge.states["1"]
		return st["ct"] == float64(447) && st["xy"] == nil
	}
	waitFor(t, "dim", dimmed)
	time.Sleep(400 * time.Millisecond) // no colours follow the picture now
	if !dimmed() {
		t.Error("theater mode should hold the lights dim")
	}
	// Off from Account: the lights go back at once.
	setMode(ModeOff)
	if _, active := a.engine.Status(); active {
		t.Error("should let go once the person turns Hue Sync off")
	}
	waitFor(t, "restore after theater", func() bool { c, ok := bridge.xy("1"); return ok && math.Abs(c.X-0.45) < 0.001 })
}

func TestRoomsFollowTheirOwnPlayers(t *testing.T) {
	video := testVideo(t)
	bridge := &fakeBridge{states: map[string]map[string]any{},
		lights: `{"1":{"name":"Sofa","type":"Extended color light","state":{"on":true,"bri":100,"xy":[0.45,0.41]}},
			"2":{"name":"Bed","type":"Extended color light","state":{"on":true,"bri":100,"xy":[0.30,0.30]}}}`,
		groups: `{"1":{"name":"Living room","type":"Room","lights":["1"]},"2":{"name":"Bedroom","type":"Room","lights":["2"]},
			"3":{"name":"Upstairs","type":"Zone","lights":["2","9"]},"4":{"name":"Hall","type":"Room","lights":["9"]}}`}
	hb := httptest.NewServer(bridge)
	defer hb.Close()

	a := &app{ffmpeg: "ffmpeg", p: &bbplugin.Plugin{Manifest: manifest}}
	a.settings = map[string]string{"updates_per_second": "10", "brightness": "100", "on_pause": "keep", "on_stop": "restore"}
	a.conf = &configStore{dir: t.TempDir}
	a.fileURL = func(int64) (string, string) { return video, "" }
	a.engine = &Engine{a: a}
	// The bed lamp isn't chosen on the setup page: picking its room is enough.
	a.conf.set(Config{Bridge: hb.URL, Username: "u1", Lights: map[string]string{"1": "all"}, People: map[string]string{"5": ModeSync}})

	// Devices see every room, then every zone.
	req := httptest.NewRequest("GET", "/areas", nil)
	req.Header.Set("X-BingeBandit-User-Id", "5")
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	var areas []struct{ ID, Name, Type string }
	json.Unmarshal(rec.Body.Bytes(), &areas)
	if len(areas) != 4 || areas[0].Name != "Bedroom" || areas[1].Name != "Hall" || areas[2].Name != "Living room" || areas[3].Type != "Zone" {
		t.Fatalf("areas = %s", rec.Body)
	}

	ctx := context.Background()
	send := func(event, session, area string, pos float64) {
		d, _ := json.Marshal(playerEvent{SessionID: session, UserID: 5, User: "ann", Client: "Android TV", ItemID: 7, FileID: 3, Title: "RGB", Position: pos, Area: area})
		a.engine.onEvent(ctx, bbplugin.Event{Event: event, Data: d})
	}
	// The living room TV drives only the sofa lamp.
	send("player.play", "living", "1", 0.2)
	waitFor(t, "sofa red", func() bool { c, ok := bridge.xy("1"); return ok && near(c, redXY[0], redXY[1]) })
	if _, ok := bridge.xy("2"); ok {
		t.Fatal("the bedroom light followed the living room TV")
	}
	// The bedroom TV, at the blue part, drives the bed lamp at the same time.
	send("player.play", "bedroom", "2", 4.4)
	waitFor(t, "bed blue", func() bool { c, ok := bridge.xy("2"); return ok && near(c, blueXY[0], blueXY[1]) })
	if status, _ := a.engine.Status(); !strings.Contains(status, "\n") {
		t.Errorf("status should list both players: %q", status)
	}
	// Stopping the bedroom puts back only the bed lamp; the living room keeps going.
	send("player.stop", "bedroom", "2", 5)
	waitFor(t, "bed restored", func() bool { c, ok := bridge.xy("2"); return ok && math.Abs(c.X-0.30) < 0.001 })
	if _, active := a.engine.Status(); !active {
		t.Fatal("the living room should still be syncing")
	}
	// A phone with every light (no area) on play takes the sofa lamp over.
	send("player.play", "phone", "", 0.2)
	a.engine.mu.Lock()
	_, living := a.engine.sessions["living"]
	a.engine.mu.Unlock()
	if living {
		t.Error("the living room player should have handed its lights to the new one")
	}
	send("player.stop", "phone", "", 1)
}

func TestDeviceMode(t *testing.T) {
	for _, c := range []struct{ device, profile, want string }{
		{"", ModeSync, ModeSync},
		{"", ModeTheater, ModeTheater},
		{ModeTheater, ModeSync, ModeTheater}, // the TV picked its own type
		{ModeSync, ModeTheater, ModeSync},
		{ModeTheater, ModeOff, ModeOff}, // the profile hasn't turned Hue Sync on
		{"bogus", ModeSync, ModeSync},
	} {
		if got := (playerEvent{Mode: c.device}).deviceMode(c.profile); got != c.want {
			t.Errorf("device %q, profile %q: got %q, want %q", c.device, c.profile, got, c.want)
		}
	}
}

// A TV that drives its own hub reads the picture's colours a stretch at a time.
func TestColoursForTV(t *testing.T) {
	video := testVideo(t)
	a := &app{ffmpeg: "ffmpeg", p: &bbplugin.Plugin{Manifest: manifest}}
	a.conf = &configStore{dir: t.TempDir}
	a.fileURL = func(int64) (string, string) { return video, "" }
	get := func(q string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/me/colours?"+q, nil)
		req.Header.Set("X-BingeBandit-User-Id", "5")
		rec := httptest.NewRecorder()
		a.routes().ServeHTTP(rec, req)
		return rec
	}
	if rec := get("fileId=3"); rec.Code != http.StatusBadRequest {
		t.Errorf("missing from and seconds: %d", rec.Code)
	}
	rec := get("fileId=3&from=4.2&seconds=1")
	var out struct {
		FPS    float64                 `json:"fps"`
		Frames []map[string][3]float64 `json:"frames"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("colours: %d %s", rec.Code, rec.Body)
	}
	if len(out.Frames) < 3 || len(out.Frames) > 5 || out.FPS != sampleFPS {
		t.Fatalf("got %d frames at %g fps, want about one second's worth", len(out.Frames), out.FPS)
	}
	if c := out.Frames[0]["all"]; !near(Colour{X: c[0], Y: c[1]}, blueXY[0], blueXY[1]) {
		t.Errorf("4.2s in is blue, got %v", c)
	}
}
