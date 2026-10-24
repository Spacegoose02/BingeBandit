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
}

func (f *fakeBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == "POST" && r.URL.Path == "/api":
		io.WriteString(w, `[{"success":{"username":"u1"}}]`)
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
	user, err := newBridge(hb.URL, "").Pair(context.Background())
	if err != nil || user != "u1" {
		t.Fatalf("pair: %q %v", user, err)
	}
	a.conf.set(Config{Bridge: hb.URL, Username: user, Lights: map[string]string{"1": "all"}})

	ctx := context.Background()
	send := func(event string, pos float64, client string) {
		d, _ := json.Marshal(playerEvent{SessionID: "s1", User: "ann", Client: client, ItemID: 7, FileID: 3, Title: "RGB", Position: pos})
		a.engine.onEvent(ctx, bbplugin.Event{Event: event, Data: d})
	}

	// A device the filter leaves out is ignored.
	send("player.play", 0.2, "Android TV")
	if _, active := a.engine.Status(); active {
		t.Fatal("should only follow the Web client")
	}

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
}
