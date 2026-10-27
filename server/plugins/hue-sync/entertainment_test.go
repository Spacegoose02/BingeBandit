package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"bingebandit/pkg/bbplugin"

	"github.com/pion/dtls/v3"
)

const testAreaID = "1a8d99cc-967b-44f2-9202-43f976c0fa6b"

func TestStreamMessage(t *testing.T) {
	area := EntArea{ID: testAreaID, Channels: []EntChannel{{ID: 0}, {ID: 3}}}
	msg := streamMessage(area, 7, []Colour{{X: 1, Y: 0, Bri: 0.5}, {X: 0.25, Y: 0.75, Bri: 1}})
	if string(msg[:9]) != "HueStream" || msg[9] != 2 || msg[10] != 0 || msg[11] != 7 || msg[14] != 1 {
		t.Fatalf("header % x", msg[:16])
	}
	if string(msg[16:52]) != testAreaID || len(msg) != 52+2*7 {
		t.Fatalf("area id or length: %q, %d", msg[16:52], len(msg))
	}
	ch := msg[52+7:]
	if ch[0] != 3 || binary.BigEndian.Uint16(ch[1:]) != 0x4000 || binary.BigEndian.Uint16(ch[5:]) != 0xffff {
		t.Errorf("second channel % x", ch)
	}
	for _, c := range []struct {
		ch   EntChannel
		zone string
	}{{EntChannel{X: -0.8}, "left"}, {EntChannel{X: 0.9, Z: 1}, "right"}, {EntChannel{Z: 0.8}, "top"}, {EntChannel{Y: 1}, "all"}} {
		if c.ch.zone() != c.zone {
			t.Errorf("%+v follows %s; want %s", c.ch, c.ch.zone(), c.zone)
		}
	}
}

// fakeEntertainment is a bridge's v2 API and its DTLS stream endpoint.
type fakeEntertainment struct {
	mu      sync.Mutex
	actions []string
	packets [][]byte
}

func (f *fakeEntertainment) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("hue-application-key") != "u1" {
		http.Error(w, "no key", http.StatusForbidden)
		return
	}
	switch {
	case r.URL.Path == "/auth/v1":
		w.Header().Set("hue-application-id", "app-1")
	case r.Method == "GET" && r.URL.Path == "/clip/v2/resource/entertainment_configuration":
		io.WriteString(w, `{"errors":[],"data":[{"id":"`+testAreaID+`","id_v1":"/groups/200","metadata":{"name":"TV area"},
			"channels":[{"channel_id":0,"position":{"x":-0.9,"y":0.8,"z":0}},{"channel_id":1,"position":{"x":0.9,"y":0.8,"z":0}}]}]}`)
	case r.Method == "PUT" && r.URL.Path == "/clip/v2/resource/entertainment_configuration/"+testAreaID:
		var body struct{ Action string }
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.actions = append(f.actions, body.Action)
		f.mu.Unlock()
		io.WriteString(w, `{"errors":[],"data":[]}`)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeEntertainment) listen(t *testing.T, key []byte) string {
	l, err := dtls.Listen("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, &dtls.Config{
		PSK: func(id []byte) ([]byte, error) {
			if string(id) != "app-1" {
				t.Errorf("PSK identity %q; want the application id", id)
			}
			return key, nil
		},
		PSKIdentityHint: []byte("bridge"),
		CipherSuites:    []dtls.CipherSuiteID{dtls.TLS_PSK_WITH_AES_128_GCM_SHA256},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 1500)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					f.mu.Lock()
					f.packets = append(f.packets, append([]byte(nil), buf[:n]...))
					f.mu.Unlock()
				}
			}()
		}
	}()
	return l.Addr().String()
}

// last is the colour the last packet gave a channel.
func (f *fakeEntertainment) last(ch int) (Colour, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.packets) == 0 {
		return Colour{}, false
	}
	p := f.packets[len(f.packets)-1]
	for i := 52; i+7 <= len(p); i += 7 {
		if int(p[i]) == ch {
			u := func(o int) float64 { return float64(binary.BigEndian.Uint16(p[i+o:])) / 0xffff }
			return Colour{X: u(1), Y: u(3), Bri: u(5)}, true
		}
	}
	return Colour{}, false
}

func TestEntertainmentStreaming(t *testing.T) {
	video := testVideo(t)
	key := []byte("0123456789abcdef")
	ent := &fakeEntertainment{}
	v2 := httptest.NewTLSServer(ent)
	defer v2.Close()
	testV2URL, testStreamAddr = v2.URL, ent.listen(t, key)
	defer func() { testV2URL, testStreamAddr = "", "" }()

	bridge := &fakeBridge{states: map[string]map[string]any{},
		groups: `{"1":{"name":"Living room","type":"Room","lights":["1","2"]},"200":{"name":"TV area","type":"Entertainment","lights":["1","2"]}}`}
	hb := httptest.NewServer(bridge)
	defer hb.Close()

	a := &app{ffmpeg: "ffmpeg", p: &bbplugin.Plugin{Manifest: manifest}}
	a.settings = map[string]string{"brightness": "100", "on_pause": "keep", "on_stop": "restore"}
	a.conf = &configStore{dir: t.TempDir}
	a.fileURL = func(int64) (string, string) { return video, "" }
	a.engine = &Engine{a: a}
	a.conf.set(Config{Bridge: hb.URL, Username: "u1", ClientKey: hex.EncodeToString(key), Lights: map[string]string{}, People: map[string]string{"5": ModeSync}})

	// Entertainment areas are offered to devices after rooms and zones.
	req := httptest.NewRequest("GET", "/areas", nil)
	req.Header.Set("X-BingeBandit-User-Id", "5")
	rec := httptest.NewRecorder()
	a.routes().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"type":"Entertainment"`) {
		t.Fatalf("areas = %s", rec.Body)
	}

	send := func(event string, pos float64) {
		d, _ := json.Marshal(playerEvent{SessionID: "s1", UserID: 5, User: "ann", Client: "Android TV", ItemID: 7, FileID: 3, Title: "RGB", Position: pos, Area: "200"})
		a.engine.onEvent(context.Background(), bbplugin.Event{Event: event, Data: d})
	}
	send("player.play", 0.2)
	waitFor(t, "streamed red", func() bool { c, ok := ent.last(1); return ok && near(c, redXY[0], redXY[1]) && c.Bri > 0.5 })
	if status, _ := a.engine.Status(); !strings.Contains(status, "streaming to TV area") {
		t.Errorf("status = %q", status)
	}
	send("player.progress", 4.4)
	waitFor(t, "streamed blue", func() bool { c, ok := ent.last(0); return ok && near(c, blueXY[0], blueXY[1]) })
	bridge.mu.Lock()
	if len(bridge.puts) != 0 {
		t.Errorf("light commands while streaming: %v", bridge.puts)
	}
	bridge.mu.Unlock()

	// Stopped: streaming ends, then the lights go back the usual way.
	send("player.stop", 5)
	waitFor(t, "stream stopped", func() bool {
		ent.mu.Lock()
		defer ent.mu.Unlock()
		return len(ent.actions) == 2 && ent.actions[0] == "start" && ent.actions[1] == "stop"
	})
	waitFor(t, "restore", func() bool { c, ok := bridge.xy("1"); return ok && c.X == 0.45 })
}
