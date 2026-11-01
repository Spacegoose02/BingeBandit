package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/cast"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/trakt"
)

// fakeTV stands in for a Chromecast or DLNA TV.
type fakeTV struct {
	mu      sync.Mutex
	loaded  []cast.Media
	status  cast.Status
	paused  bool
	seeks   []float64
	stopped bool
}

func (f *fakeTV) Load(_ context.Context, m cast.Media) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded = append(f.loaded, m)
	f.status = cast.Status{State: cast.StatePlaying, Position: m.Start, Volume: 0.5}
	return nil
}
func (f *fakeTV) Play(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status.State = cast.StatePlaying
	return nil
}
func (f *fakeTV) Pause(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = true
	f.status.State = cast.StatePaused
	return nil
}
func (f *fakeTV) Seek(_ context.Context, pos float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seeks = append(f.seeks, pos)
	f.status.Position = pos
	return nil
}
func (f *fakeTV) SetVolume(context.Context, float64) error { return nil }
func (f *fakeTV) Status(context.Context) (cast.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}
func (f *fakeTV) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return nil
}
func (f *fakeTV) Close() {}

func (f *fakeTV) set(st cast.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = st
}

func (f *fakeTV) last() cast.Media {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loaded[len(f.loaded)-1]
}

type castFixture struct {
	t      *testing.T
	st     *store.Store
	h      http.Handler
	tv     *fakeTV
	item   int64
	file   *store.MediaFile
	other  *store.MediaFile
	dir    string
	viewer *store.User
}

func newCastFixture(t *testing.T, transcoder bool) *castFixture {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	viewer, _ := st.CreateUser("sam", string(hash), true)
	st.CreateSession("sam-token", viewer.ID, "Phone", "")
	// The server is told where TVs reach it; working it out needs a network.
	st.SetSettings(map[string]string{store.SettingCastURL: "http://192.168.1.10:8484"})
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)

	f := &castFixture{t: t, st: st, tv: &fakeTV{status: cast.Status{State: cast.StateIdle}}, dir: dir, viewer: viewer}
	add := func(title string, mf store.MediaFile) (int64, *store.MediaFile) {
		id, err := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: 2026, MatchKey: "movie:" + title})
		if err != nil {
			t.Fatal(err)
		}
		mf.ItemID = id
		if _, err := os.Stat(mf.Path); err != nil {
			os.WriteFile(mf.Path, []byte("not really a video"), 0o644)
		}
		if err := st.UpsertFile(&mf, 1); err != nil {
			t.Fatal(err)
		}
		got, err := st.FileByPath(mf.Path)
		if err != nil {
			t.Fatal(err)
		}
		return id, got
	}
	f.item, f.file = add("Clip", store.MediaFile{Path: filepath.Join(dir, "clip.mp4"), Size: 1, Container: "mp4", Duration: 3600,
		VideoCodec: "h264", Width: 1920, Height: 1080, AudioCodec: "aac", AudioChannels: 2})
	_, f.other = add("Other", store.MediaFile{Path: filepath.Join(dir, "other.mp4"), Size: 1, Container: "mp4", Duration: 60, VideoCodec: "h264", AudioCodec: "aac"})

	oldDiscover, oldOpen := castDiscover, castOpen
	castDiscover = func(context.Context, time.Duration, []string) []cast.Device {
		return []cast.Device{
			{ID: "cc-living", Name: "Living Room", Kind: cast.KindChromecast, Host: "192.168.1.50"},
			{ID: "dlna-bedroom", Name: "Bedroom TV", Kind: cast.KindDLNA, Host: "192.168.1.51"},
		}
	}
	castOpen = func(context.Context, cast.Device) (cast.Player, error) { return f.tv, nil }
	t.Cleanup(func() { castDiscover, castOpen = oldDiscover, oldOpen })

	srv := &Server{Store: st, DataDir: dir, Activity: stream.NewActivity(), Trakt: trakt.NewSyncer(st, "")}
	if transcoder {
		ffmpeg, err := exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("ffmpeg not installed")
		}
		srv.Transcoder = stream.NewManager(st, ffmpeg, dir)
		t.Cleanup(srv.Transcoder.Shutdown)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.h = srv.Router(ctx)
	return f
}

func (f *castFixture) call(token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func (f *castFixture) session(id string) (castView, int) {
	rec := f.call("sam-token", "GET", "/api/cast/sessions/"+id, "")
	var c castView
	json.Unmarshal(rec.Body.Bytes(), &c)
	return c, rec.Code
}

func (f *castFixture) waitFor(what string, ok func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A phone casts an MP4 to a Chromecast: the TV gets a direct link whose
// token opens only this file, the remote works, the position is saved, and
// stopping ends the token.
func TestCastDirect(t *testing.T) {
	f := newCastFixture(t, false)
	rec := f.call("sam-token", "GET", "/api/cast/devices", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Living Room") || strings.Contains(rec.Body.String(), "controlURL") {
		t.Fatalf("devices: %d %s", rec.Code, rec.Body)
	}
	if c := f.call("", "GET", "/api/cast/devices", "").Code; c != http.StatusUnauthorized {
		t.Fatalf("devices without signing in: %d", c)
	}

	rec = f.call("sam-token", "POST", "/api/cast/start", `{"deviceId":"cc-living","itemId":`+itoa(f.item)+`,"start":120}`)
	var c castView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &c) != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if c.Mode != "direct" || c.Title != "Clip" || c.Device.Name != "Living Room" {
		t.Fatalf("session: %+v", c)
	}
	m := f.tv.last()
	if !strings.HasPrefix(m.URL, "http://192.168.1.10:8484/api/files/"+itoa(f.file.ID)+"/direct?token=") || m.ContentType != "video/mp4" || m.Start != 120 {
		t.Fatalf("loaded: %+v", m)
	}
	token := m.URL[strings.Index(m.URL, "token=")+len("token="):]

	// The token opens this file, and nothing else.
	if c := f.call("", "GET", "/api/files/"+itoa(f.file.ID)+"/direct?token="+token, "").Code; c != http.StatusOK {
		t.Fatalf("TV opening its file: %d", c)
	}
	if c := f.call("", "HEAD", "/api/files/"+itoa(f.file.ID)+"/direct?token="+token, "").Code; c != http.StatusOK {
		t.Fatalf("TV asking about its file: %d", c)
	}
	for _, p := range []string{"/api/files/" + itoa(f.other.ID) + "/direct", "/api/me", "/api/cast/sessions"} {
		if c := f.call("", "GET", p+"?token="+token, "").Code; c != http.StatusUnauthorized {
			t.Fatalf("token on %s: %d", p, c)
		}
	}

	// The TV plays; the server follows it and saves the position.
	f.tv.set(cast.Status{State: cast.StatePlaying, Position: 600, Volume: 0.4})
	f.waitFor("the position", func() bool { s, _ := f.session(c.ID); return s.Position == 600 && s.State == cast.StatePlaying })
	f.waitFor("progress saved", func() bool {
		it, err := f.st.Item(f.viewer.ID, f.item)
		return err == nil && it.Watch.Position >= 600
	})

	// The remote.
	if rec := f.call("sam-token", "POST", "/api/cast/sessions/"+c.ID, `{"action":"pause"}`); rec.Code != http.StatusOK || !f.tv.paused {
		t.Fatalf("pause: %d %s", rec.Code, rec.Body)
	}
	if rec := f.call("sam-token", "POST", "/api/cast/sessions/"+c.ID, `{"action":"seek","position":900}`); rec.Code != http.StatusOK || len(f.tv.seeks) != 1 || f.tv.seeks[0] != 900 {
		t.Fatalf("seek: %d %s %v", rec.Code, rec.Body, f.tv.seeks)
	}
	if rec := f.call("sam-token", "POST", "/api/cast/sessions/"+c.ID, `{"action":"dance"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: %d", rec.Code)
	}
	rec = f.call("sam-token", "GET", "/api/cast/sessions", "")
	if !strings.Contains(rec.Body.String(), c.ID) || strings.Contains(rec.Body.String(), token) {
		t.Fatalf("session list: %s", rec.Body)
	}

	if c := f.call("sam-token", "DELETE", "/api/cast/sessions/"+c.ID, "").Code; c != http.StatusNoContent {
		t.Fatalf("stop: %d", c)
	}
	if _, code := f.session(c.ID); code != http.StatusNotFound {
		t.Fatalf("session after stop: %d", code)
	}
	if c := f.call("", "GET", "/api/files/"+itoa(f.file.ID)+"/direct?token="+token, "").Code; c != http.StatusUnauthorized {
		t.Fatalf("token after stop: %d", c)
	}
	f.waitFor("the TV stopped", func() bool { f.tv.mu.Lock(); defer f.tv.mu.Unlock(); return f.tv.stopped })
}

// Played to the end on the TV: the cast ends by itself and the title
// counts as watched.
func TestCastFinishes(t *testing.T) {
	f := newCastFixture(t, false)
	rec := f.call("sam-token", "POST", "/api/cast/start", `{"deviceId":"dlna-bedroom","itemId":`+itoa(f.item)+`}`)
	var c castView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &c) != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	f.tv.set(cast.Status{State: cast.StatePlaying, Position: 3500})
	f.waitFor("playing", func() bool { s, _ := f.session(c.ID); return s.Position == 3500 })
	f.tv.set(cast.Status{State: cast.StateIdle}) // DLNA: STOPPED at the end
	f.waitFor("the cast to end", func() bool { _, code := f.session(c.ID); return code == http.StatusNotFound })
	it, err := f.st.Item(f.viewer.ID, f.item)
	if err != nil || !it.Watch.Watched {
		t.Fatalf("watched after finishing: %+v %v", it.Watch, err)
	}
}

// Starting a cast checks the TV and the title.
func TestCastStartRefusals(t *testing.T) {
	f := newCastFixture(t, false)
	for body, want := range map[string]int{
		`{"deviceId":"nope","itemId":` + itoa(f.item) + `}`:           http.StatusNotFound,
		`{"deviceId":"cc-living","itemId":999}`:                       http.StatusNotFound,
		`{"itemId":` + itoa(f.item) + `}`:                             http.StatusBadRequest,
		`{"deviceId":"cc-living","itemId":` + itoa(f.item) + `,"x":}`: http.StatusBadRequest,
	} {
		if rec := f.call("sam-token", "POST", "/api/cast/start", body); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", body, rec.Code, rec.Body, want)
		}
	}
}

func TestCastSettings(t *testing.T) {
	for _, tc := range []struct {
		in   map[string]string
		ok   bool
		want map[string]string
	}{
		{map[string]string{"cast_url": "http://192.168.1.10:8484/"}, true, map[string]string{"cast_url": "http://192.168.1.10:8484"}},
		{map[string]string{"cast_url": "192.168.1.10"}, false, nil},
		{map[string]string{"cast_url": "http://x/y"}, false, nil},
		{map[string]string{"cast_devices": "192.168.1.50\n 192.168.1.51:8009 , 10.0.0.2"}, true, map[string]string{"cast_devices": "192.168.1.50\n192.168.1.51\n10.0.0.2"}},
		{map[string]string{"cast_devices": "tv.local"}, false, nil},
	} {
		err := validCastSettings(tc.in)
		if (err == nil) != tc.ok {
			t.Errorf("%v: %v", tc.in, err)
		}
		for k, v := range tc.want {
			if tc.in[k] != v {
				t.Errorf("%s = %q, want %q", k, tc.in[k], v)
			}
		}
	}
}

// A DLNA TV gets a file it can't play as one MPEG-TS stream of the
// conversion, which the token opens.
func TestCastConvertedStream(t *testing.T) {
	f := newCastFixture(t, true)
	ffmpeg, _ := exec.LookPath("ffmpeg")
	mkv := filepath.Join(f.dir, "clip.mkv")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", mkv).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	if err := f.st.UpsertFile(&store.MediaFile{ItemID: f.item, Path: mkv, Size: 2, Container: "matroska", Duration: 10,
		VideoCodec: "h264", Width: 320, Height: 240}, 1); err != nil {
		t.Fatal(err)
	}
	mf, _ := f.st.FileByPath(mkv)
	rec := f.call("sam-token", "POST", "/api/cast/start", `{"deviceId":"dlna-bedroom","itemId":`+itoa(f.item)+`,"fileId":`+itoa(mf.ID)+`}`)
	var c castView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &c) != nil {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	m := f.tv.last()
	if c.Mode != "transcode" || m.ContentType != "video/mpeg" || !strings.Contains(m.URL, "/stream.ts?token=") {
		t.Fatalf("loaded %+v (mode %s)", m, c.Mode)
	}
	path := strings.TrimPrefix(m.URL, "http://192.168.1.10:8484")
	if rec := f.call("", "HEAD", path, ""); rec.Code != http.StatusOK || rec.Header().Get("transferMode.dlna.org") != "Streaming" {
		t.Fatalf("HEAD stream: %d %v", rec.Code, rec.Header())
	}
	rec = f.call("", "GET", path, "")
	body := rec.Body.Bytes()
	if rec.Code != http.StatusOK || len(body) < 188*10 || body[0] != 0x47 || !bytes.Equal(body[188:189], []byte{0x47}) {
		t.Fatalf("stream: %d, %d bytes", rec.Code, len(body))
	}

	// A Chromecast gets the HLS playlist, with the token on every segment
	// and CORS for its player.
	rec = f.call("sam-token", "POST", "/api/cast/start", `{"deviceId":"cc-living","itemId":`+itoa(f.item)+`,"fileId":`+itoa(mf.ID)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("start on the Chromecast: %d %s", rec.Code, rec.Body)
	}
	m = f.tv.last()
	if m.ContentType != "application/x-mpegURL" || !strings.Contains(m.URL, "/index.m3u8?token=") {
		t.Fatalf("loaded %+v", m)
	}
	rec = f.call("", "GET", strings.TrimPrefix(m.URL, "http://192.168.1.10:8484"), "")
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(rec.Body.String(), "seg00000.ts?token=") {
		t.Fatalf("playlist: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
}
