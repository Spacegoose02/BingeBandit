package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
	s := &Server{Store: st}
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
	if len(lineup) != 1 || lineup[0].GuideNumber != "7" || !strings.Contains(lineup[0].URL, "stream.ts?token="+key) {
		t.Fatalf("lineup: %s", rec.Body)
	}

	rec = get(base + "/epg.xml")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<display-name>7</display-name>`) {
		t.Fatalf("guide: %d %s", rec.Code, rec.Body)
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
