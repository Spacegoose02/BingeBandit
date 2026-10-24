package livetv

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
