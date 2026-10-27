package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

func TestEPGPreview(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := st.CreateAPIKey(admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	src := &store.LiveSource{Kind: "epg", Name: "Guide", URL: "http://guide"}
	if err := st.SaveLiveSource(src); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	gi, err := st.BeginGuideImport(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	gi.Channel("news.us", "News US", "")
	gi.Channel("news.uk", "News UK", "")
	for _, p := range []store.GuideProgram{
		{EPGID: "news.us", Start: now - 7200, Stop: now - 600, Title: "Earlier"},
		{EPGID: "news.us", Start: now - 600, Stop: now + 1200, Title: "Morning News"},
		{EPGID: "news.us", Start: now + 1200, Stop: now + 3000, Title: "Weather"},
		{EPGID: "news.us", Start: now + 8*3600, Stop: now + 9*3600, Title: "Too late"},
		{EPGID: "news.uk", Start: now - 60, Stop: now + 60, Title: "Breakfast"},
	} {
		if err := gi.Program(&p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gi.Finish(); err != nil {
		t.Fatal(err)
	}

	h := (&Server{Store: st, Live: livetv.NewManager(st, "ffmpeg", "ffprobe", t.TempDir())}).Router(context.Background())
	get := func(path string, v any) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}

	var progs []store.GuideProgram
	get("/api/admin/live/epg-preview?id=news.us", &progs)
	if len(progs) != 2 || progs[0].Title != "Morning News" || progs[1].Title != "Weather" {
		t.Fatalf("preview = %+v", progs)
	}

	var chans []struct {
		ID  string `json:"id"`
		Now string `json:"now"`
	}
	get("/api/admin/live/epg-channels?q=news", &chans)
	got := map[string]string{}
	for _, c := range chans {
		got[c.ID] = c.Now
	}
	if got["news.us"] != "Morning News" || got["news.uk"] != "Breakfast" {
		t.Fatalf("search now = %v", got)
	}
}
