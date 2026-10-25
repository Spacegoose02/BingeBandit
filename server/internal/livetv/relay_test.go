package livetv

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestRelayFallbackAndSharing(t *testing.T) {
	var goodConns atomic.Int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodConns.Add(1)
		w.WriteHeader(http.StatusOK)
		chunk := make([]byte, 188*20)
		for {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer bad.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: "ffmpeg", ffprobe: "ffprobe", dataDir: t.TempDir(), relays: map[int64]*Relay{}}

	ch := &store.Channel{ID: 1, Number: 5, Name: "Test", Streams: []store.ChannelStream{
		{StreamID: 1, Name: "broken", SourceID: 1, URL: bad.URL},
		{StreamID: 2, Name: "working", SourceID: 2, URL: good.URL},
	}}
	user := &store.User{ID: 1, Username: "a"}
	v1, err := m.Join(ch, user, "127.0.0.1", "ts")
	if err != nil {
		t.Fatal(err)
	}
	v2, _ := m.Join(ch, user, "127.0.0.2", "ts")

	for _, v := range []*Viewer{v1, v2} {
		select {
		case c := <-v.Chunks():
			if len(c) == 0 {
				t.Fatal("empty chunk")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("viewer got no data: fallback to the working stream failed")
		}
	}
	if n := goodConns.Load(); n != 1 {
		t.Errorf("two viewers opened %d upstream connections, want 1 shared", n)
	}
	stats := m.Stats()
	if len(stats) != 1 || stats[0].Stream != "working" || len(stats[0].Viewers) != 2 || stats[0].Failovers != 1 {
		t.Errorf("stats = %+v", stats)
	}

	// A second channel on the same provider is refused while someone is
	// watching the first, when the provider allows one connection; after a
	// short wait it gives up and stops.
	limited := &store.Channel{ID: 2, Number: 6, Name: "Other", Streams: []store.ChannelStream{
		{StreamID: 3, Name: "same provider", SourceID: 2, URL: good.URL, MaxStreams: 1},
	}}
	v3, _ := m.Join(limited, user, "127.0.0.3", "ts")
	select {
	case <-v3.Done():
	case <-time.After(10 * time.Second):
		t.Error("channel over the provider's connection limit should stop")
	}

	// Once everyone has flipped away from the first channel, its idle
	// connection is handed to the channel being tuned instead of failing.
	first := m.relay(1)
	m.Leave(v1.ID)
	m.Leave(v2.ID)
	v4, _ := m.Join(limited, user, "127.0.0.4", "ts")
	select {
	case <-v4.Chunks():
	case <-time.After(5 * time.Second):
		t.Fatal("an idle relay's connection wasn't reclaimed for the new channel")
	}
	if !first.stopped.Load() {
		t.Error("the idle relay should have been stopped")
	}

	m.Stop(2)
	select {
	case <-v4.Done():
	case <-time.After(2 * time.Second):
		t.Error("stopping the relay should disconnect viewers")
	}
}

// pcrServer sends MPEG-TS whose clock runs at rate × real time.
func pcrServer(rate float64, conns *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conns.Add(1)
		w.WriteHeader(http.StatusOK)
		media := 10.0
		for {
			chunk := tsWithPCR(256, media)
			for i := 0; i < 19; i++ {
				chunk = append(chunk, tsWithPCR(257, 0)[:188]...) // filler packets
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			media += 0.1 * rate
		}
	}))
}

func TestSlowStreamFailsOver(t *testing.T) {
	var slowConns, goodConns atomic.Int32
	slow := pcrServer(0.3, &slowConns)
	defer slow.Close()
	good := pcrServer(1, &goodConns)
	defer good.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// Switch once a player with a 2 s cushion would have buffered for 3 s.
	if err := st.SetSettings(map[string]string{store.SettingLiveStallSeconds: "3", store.SettingLiveBufferSeconds: "2"}); err != nil {
		t.Fatal(err)
	}
	m := &Manager{st: st, ffmpeg: "ffmpeg", ffprobe: "ffprobe", dataDir: t.TempDir(), relays: map[int64]*Relay{}}
	ch := &store.Channel{ID: 1, Number: 5, Name: "Test", Streams: []store.ChannelStream{
		{StreamID: 1, Name: "slow", SourceID: 1, URL: slow.URL},
		{StreamID: 2, Name: "good", SourceID: 2, URL: good.URL},
	}}
	v, err := m.Join(ch, &store.User{ID: 1, Username: "a"}, "127.0.0.1", "ts")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(1)
	go func() { // keep the viewer drained
		for range v.Chunks() {
		}
	}()
	deadline := time.Now().Add(15 * time.Second)
	for goodConns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if goodConns.Load() == 0 {
		t.Fatal("a stream playing at 30% speed never failed over to the good one")
	}
	if s := m.Stats(); len(s) != 1 || !strings.Contains(s[0].LastError, "too slow") {
		t.Errorf("last error should say the stream was too slow: %+v", s)
	}
	// The slow stream is set aside: it's tried last, and the channel won't
	// leave the good one for it because of slowness.
	r := m.relay(1)
	if order := r.passOrder(); order[0].Name != "good" {
		t.Errorf("the slow stream should be tried last, got %v first", order[0].Name)
	}
	r.mu.Lock()
	leave := r.canLeaveSlow()
	r.mu.Unlock()
	if leave {
		t.Error("would leave the good stream for the one just found too slow")
	}
}

func TestSlowSwitchingOff(t *testing.T) {
	var slowConns, goodConns atomic.Int32
	slow := pcrServer(0.3, &slowConns)
	defer slow.Close()
	good := pcrServer(1, &goodConns)
	defer good.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSettings(map[string]string{store.SettingLiveStallSeconds: "3", store.SettingLiveBufferSeconds: "2",
		store.SettingLiveSwitchSlow: "false"}); err != nil {
		t.Fatal(err)
	}
	m := &Manager{st: st, ffmpeg: "ffmpeg", ffprobe: "ffprobe", dataDir: t.TempDir(), relays: map[int64]*Relay{}}
	ch := &store.Channel{ID: 1, Number: 5, Name: "Test", Streams: []store.ChannelStream{
		{StreamID: 1, Name: "slow", SourceID: 1, URL: slow.URL},
		{StreamID: 2, Name: "good", SourceID: 2, URL: good.URL},
	}}
	v, err := m.Join(ch, &store.User{ID: 1, Username: "a"}, "127.0.0.1", "ts")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(1)
	go func() {
		for range v.Chunks() {
		}
	}()
	time.Sleep(10 * time.Second)
	if goodConns.Load() != 0 {
		t.Error("switched for slowness with switching on slowness turned off")
	}
	if ok, _ := m.ReportBuffering(v.ID, 30); ok {
		t.Error("a buffering report switched with switching on slowness turned off")
	}
}

func TestBufferingReport(t *testing.T) {
	var aConns, bConns atomic.Int32
	a := pcrServer(1, &aConns)
	defer a.Close()
	b := pcrServer(1, &bConns)
	defer b.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: "ffmpeg", ffprobe: "ffprobe", dataDir: t.TempDir(), relays: map[int64]*Relay{}}
	ch := &store.Channel{ID: 1, Number: 5, Name: "Test", Streams: []store.ChannelStream{
		{StreamID: 1, Name: "a", SourceID: 1, URL: a.URL},
		{StreamID: 2, Name: "b", SourceID: 2, URL: b.URL},
	}}
	v, _ := m.Join(ch, &store.User{ID: 1, Username: "a"}, "127.0.0.1", "ts")
	defer m.Stop(1)
	go func() {
		for range v.Chunks() {
		}
	}()
	time.Sleep(1500 * time.Millisecond)
	if ok, why := m.ReportBuffering(v.ID, 5); ok {
		t.Errorf("5s of buffering is under the 12s default but switched: %s", why)
	}
	if ok, why := m.ReportBuffering(v.ID, 13); ok {
		t.Errorf("the stream keeps up, so one viewer's buffering shouldn't switch it: %s", why)
	}
	if bConns.Load() != 0 {
		t.Error("switched to the second stream without cause")
	}
}
