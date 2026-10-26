package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

func TestTunerForPlex(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, err := st.CreateUser("viewer", "", false)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := st.CreateAPIKey(u.ID, "Plex")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChannel(&store.Channel{Number: 7, Name: "News", EPGID: "news.us", Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	// No guide data at all, and a second channel on the same guide channel.
	if err := st.CreateChannel(&store.Channel{Number: 8, Name: "Local Feed", Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChannel(&store.Channel{Number: 9, Name: "News West", EPGID: "news.us", Enabled: true}, nil); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Live: livetv.NewManager(st, "ffmpeg", "ffprobe", t.TempDir())}
	h := s.Router(context.Background())
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}
	base := "/api/live/tuner/" + key

	rec := get(base + "/discover.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("discover: %d %s", rec.Code, rec.Body)
	}
	var disc map[string]any
	json.Unmarshal(rec.Body.Bytes(), &disc)
	if disc["LineupURL"] != "http://example.com"+base+"/lineup.json" {
		t.Fatalf("lineup URL: %v", disc["LineupURL"])
	}

	rec = get(base + "/lineup.json")
	var lineup []struct{ GuideNumber, GuideName, URL string }
	json.Unmarshal(rec.Body.Bytes(), &lineup)
	if len(lineup) != 3 || lineup[0].GuideNumber != "7" || !strings.HasSuffix(lineup[0].URL, base+"/channels/1/stream.ts") {
		t.Fatalf("lineup: %s", rec.Body)
	}

	rec = get(base + "/epg.xml")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<display-name>7</display-name>`) {
		t.Fatalf("guide: %d %s", rec.Code, rec.Body)
	}
	// Plex can't play a channel without a programme on now, so every
	// channel is in the guide, filled with placeholders where there's none.
	for _, want := range []string{`<channel id="news.us">`, `<channel id="bb.2">`, `<channel id="bb.3">`, `channel="bb.2">`, `<title>Local Feed</title>`, `channel="bb.3">`, `<title>News West</title>`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("guide is missing %s", want)
		}
	}

	// Plex appends "?transcode=heavy" to stream links whatever they hold.
	// The channels have no streams, so getting past sign-in means a 503.
	for _, u := range []string{base + "/channels/1/stream.ts?transcode=heavy", "/api/live/channels/1/stream.ts?token=" + key + "?transcode=heavy"} {
		if c := get(u).Code; c != http.StatusServiceUnavailable {
			t.Errorf("%s: %d", u, c)
		}
	}

	if c := get("/api/live/tuner/wrong-key/epg.xml").Code; c != http.StatusUnauthorized {
		t.Fatalf("bad key: %d", c)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", base+"/lineup.post?scan=start", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("lineup.post: %d", rec.Code)
	}
}

func TestFillGuide(t *testing.T) {
	progs := []store.GuideProgram{
		{Start: 3600, Stop: 5400, Title: "A"},
		{Start: 9000, Stop: 10800, Title: "B"}, // a gap from 5400 to 9000 before it
	}
	got := fillGuide(progs, "Ch", 1800, 14400)
	var b strings.Builder
	for _, p := range got {
		b.WriteString(p.Title + " ")
	}
	if b.String() != "Ch A Ch Ch B Ch " {
		t.Fatalf("got %q", b.String())
	}
	for i := 1; i < len(got); i++ {
		if got[i].Start != got[i-1].Stop {
			t.Fatalf("gap or overlap at %d: %+v", i, got)
		}
	}
	if got[0].Start != 1800 || got[len(got)-1].Stop != 14400 {
		t.Fatalf("doesn't cover the window: %+v", got)
	}
}

func TestEpisodeNums(t *testing.T) {
	at := time.Date(2026, 10, 26, 18, 0, 0, 0, time.UTC).Unix()
	got := episodeNums(store.GuideProgram{Start: at, Episode: "S1E2", Category: "Comedy"})
	want := []xmlEpisode{{"onscreen", "S1E2"}, {"original-air-date", "2026-10-26 18:00:00"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("show: got %v, want %v", got, want)
	}
	if got := episodeNums(store.GuideProgram{Start: at, Episode: "0.1.", Category: "Movie"}); !reflect.DeepEqual(got, []xmlEpisode{{"xmltv_ns", "0.1."}}) {
		t.Errorf("movie: got %v", got)
	}
	if got := episodeNums(store.GuideProgram{Start: at}); len(got) != 1 || got[0].System != "original-air-date" {
		t.Errorf("filler: got %v", got)
	}
}
