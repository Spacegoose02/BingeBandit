package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// TestPlayerEvents checks the live player.* events that follow /api/progress pings.
func TestPlayerEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ann, _ := st.CreateUser("ann", "x", false)
	st.CreateSession("ann-token", ann.ID, "test", "127.0.0.1")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	movie, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Year: 1995, MatchKey: "movie:heat:1995"})

	got := make(chan map[string]any, 32)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		got <- body
	}))
	defer hook.Close()
	st.SaveWebhook(&store.Webhook{Name: "t", URL: hook.URL, Enabled: true, Events: []string{"player.play", "player.pause", "player.resume", "player.stop"}})

	s := &Server{Store: st, Activity: stream.NewActivity(), Version: "test"}
	srv := httptest.NewServer(s.Router(ctx))
	defer srv.Close()
	ping := func(paused, stopped bool, pos float64) {
		b, _ := json.Marshal(map[string]any{"playSessionId": "p1", "itemId": movie, "fileId": 9, "position": pos, "duration": 6000, "paused": paused, "stopped": stopped, "client": "Web"})
		req, _ := http.NewRequest("POST", srv.URL+"/api/progress", strings.NewReader(string(b)))
		req.Header.Set("Authorization", "Bearer ann-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("progress: %v %v", err, resp)
		}
	}
	next := func(want string) map[string]any {
		t.Helper()
		select {
		case e := <-got:
			if e["event"] != want {
				t.Fatalf("got %v, want %s", e["event"], want)
			}
			return e["data"].(map[string]any)
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s", want)
		}
		return nil
	}

	ping(false, false, 1) // the very first ping, long before playback.start's 30 s
	d := next("player.play")
	if d["sessionId"] != "p1" || d["fileId"] != float64(9) || d["user"] != "ann" {
		t.Errorf("player.play data: %v", d)
	}
	ping(false, false, 11) // still playing: nothing for webhooks (player.progress is plugin-only)
	ping(true, false, 12)
	next("player.pause")
	ping(false, false, 12)
	next("player.resume")
	ping(false, true, 20)
	if d := next("player.stop"); d["reason"] != "stopped" {
		t.Errorf("stop reason: %v", d["reason"])
	}

	// A player that goes quiet is reported stopped when it expires.
	ping(false, false, 1)
	next("player.play")
	np, _ := s.Activity.Get("p1")
	s.playerExpired(np) // what Activity.OnExpire calls; expiry itself is tested in package stream
	if d := next("player.stop"); d["reason"] != "timeout" {
		t.Errorf("expiry reason: %v", d["reason"])
	}
	select {
	case e := <-got:
		t.Errorf("unexpected %v", e["event"])
	case <-time.After(300 * time.Millisecond):
	}
}
