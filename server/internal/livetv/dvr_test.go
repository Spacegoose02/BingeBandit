package livetv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestFindConflicts(t *testing.T) {
	stream := func(source int64, name string, max int) store.ChannelStream {
		return store.ChannelStream{SourceID: source, SourceName: name, MaxStreams: max}
	}
	channels := map[int64]*store.Channel{
		1: {ID: 1, Streams: []store.ChannelStream{stream(1, "Provider A", 1)}},
		2: {ID: 2, Streams: []store.ChannelStream{stream(1, "Provider A", 1)}},
		3: {ID: 3, Streams: []store.ChannelStream{stream(1, "Provider A", 1), stream(2, "Provider B", 1)}},
		4: {ID: 4, Custom: true},
		5: {ID: 5},
	}
	rec := func(id, channel, start, stop int64) *store.Recording {
		return &store.Recording{ID: id, ChannelID: channel, Title: "Show " + string(rune('A'+id-1)), StartAt: start, StopAt: stop}
	}
	got := findConflicts([]*store.Recording{
		rec(1, 1, 100, 200),
		rec(2, 1, 150, 250), // same channel: shares the connection
		rec(3, 2, 120, 180), // provider A is taken by channel 1
		rec(4, 3, 130, 190), // falls back to provider B
		rec(5, 4, 100, 300), // custom channel needs no connection
		rec(6, 2, 250, 300), // channel 1 is done by then
		rec(7, 5, 100, 120), // no streams
		rec(8, 9, 100, 120), // deleted channel
	}, channels)
	if len(got) != 3 || got[3] == "" || got[7] == "" || got[8] == "" {
		t.Fatalf("conflicts = %v, want 3, 7 and 8", got)
	}
	if !strings.Contains(got[3], "Provider A allows 1 connection") || !strings.Contains(got[3], "Show A") {
		t.Errorf("message = %q", got[3])
	}

	// With provider B taken as well, channel 3 has nowhere to go.
	got = findConflicts([]*store.Recording{
		rec(1, 1, 100, 200),
		{ID: 2, ChannelID: 6, Title: "B", StartAt: 100, StopAt: 200},
		rec(3, 3, 150, 160),
	}, map[int64]*store.Channel{1: channels[1], 3: channels[3], 6: {ID: 6, Streams: []store.ChannelStream{stream(2, "Provider B", 1)}}})
	if !strings.HasPrefix(got[3], "every provider") || len(got) != 1 {
		t.Errorf("conflicts = %v", got)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	st.SetSettings(map[string]string{store.SettingDVRFolder: dir})
	u, err := st.CreateUser("a", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	rule, err := st.CreateRule(&store.RecordingRule{UserID: u.ID, Title: "News", Status: "active", KeepLast: 2})
	if err != nil {
		t.Fatal(err)
	}
	ch := &store.Channel{Number: 1, Name: "C", Enabled: true}
	if err := st.SaveCustomChannel(ch, nil); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere.ts")
	os.WriteFile(outside, []byte("x"), 0o644)
	var paths []string
	for i := range 4 {
		r, err := st.CreateRecording(&store.Recording{UserID: u.ID, RuleID: rule.ID, ChannelID: ch.ID, ChannelName: "C", Title: "News",
			ProgramStart: int64(1000 + i*100), ProgramStop: int64(1050 + i*100), StartAt: int64(1000 + i*100), StopAt: int64(1050 + i*100), Status: "scheduled"})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "News", "Season 01", "ep"+string(rune('0'+i))+".ts")
		if i == 1 {
			path = outside // a file someone moved out of the folder is never deleted
		} else {
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte("x"), 0o644)
		}
		st.SetRecordingState(r.ID, "completed", path, 1, "")
		paths = append(paths, path)
	}
	rec := &Recorder{st: st, dataDir: t.TempDir()}
	rec.Prune(rule.ID)

	recs, _ := st.Recordings(0)
	if len(recs) != 2 || recs[0].ProgramStart != 1300 || recs[1].ProgramStart != 1200 {
		t.Fatalf("kept %d recordings: %+v", len(recs), recs)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Error("the oldest file should be deleted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("a file outside the recordings folder must be left alone")
	}
	if _, err := os.Stat(paths[2]); err != nil {
		t.Error("kept recordings' files must stay")
	}
	if _, err := os.Stat(filepath.Join(dir, "News")); err != nil {
		t.Error("folders that still hold recordings must stay")
	}

	// Keep 0 means keep everything.
	st.SetRuleKeep(rule.ID, 0)
	rec.Prune(rule.ID)
	if recs, _ := st.Recordings(0); len(recs) != 2 {
		t.Errorf("keep 0 deleted recordings: %d left", len(recs))
	}
}

// A recording may take a provider's only connection from live viewers when
// the admin allows it, and is refused when not.
func TestRecordingPreemptsLiveViewers(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer up.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: "ffmpeg", ffprobe: "ffprobe", dataDir: t.TempDir(), relays: map[int64]*Relay{}}
	channel := func(id int64) *store.Channel {
		return &store.Channel{ID: id, Name: "Ch", Streams: []store.ChannelStream{{StreamID: id, Name: "s", SourceID: 1, URL: up.URL, MaxStreams: 1}}}
	}
	user := &store.User{ID: 1, Username: "a"}
	watcher, _ := m.Join(channel(1), user, "127.0.0.1", "ts")
	select {
	case <-watcher.Chunks():
	case <-time.After(5 * time.Second):
		t.Fatal("live viewer got no data")
	}

	// Off: the recording can't get the connection.
	dvr, _ := m.Join(channel(2), user, dvrClient, "ts")
	select {
	case <-dvr.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("recording should have been refused with priority off")
	}
	if !strings.Contains(m.ChannelError(2, ""), "connection limit") {
		t.Errorf("channel error = %q", m.ChannelError(2, ""))
	}

	// On: the live viewer is bumped and the recording gets data.
	st.SetSettings(map[string]string{store.SettingDVRPriority: "true"})
	dvr, _ = m.Join(channel(2), user, dvrClient, "ts")
	select {
	case <-dvr.Chunks():
	case <-time.After(5 * time.Second):
		t.Fatal("recording didn't take the connection")
	}
	select {
	case <-watcher.Done():
	case <-time.After(2 * time.Second):
		t.Error("the live viewer should have been disconnected")
	}
	m.Stop(2)
}
