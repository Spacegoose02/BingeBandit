package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeArr is enough of Radarr or Sonarr for the tests.
type fakeArr struct {
	mu    sync.Mutex
	kind  string
	calls []string
}

func (f *fakeArr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-Api-Key") != "key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI()+" "+string(body))
	p := strings.TrimPrefix(r.URL.Path, "/api/v3")
	switch {
	case p == "/qualityprofile":
		io.WriteString(w, `[{"id":1,"name":"Any"},{"id":4,"name":"HD-1080p"}]`)
	case p == "/rootfolder":
		io.WriteString(w, `[{"id":1,"path":"/data/media"},{"id":2,"path":"/tv"}]`)
	case p == "/movie" && r.Method == "GET" && r.URL.Query().Get("tmdbId") == "949":
		io.WriteString(w, `[{"id":7,"title":"Heat","tmdbId":949,"monitored":true}]`)
	case p == "/movie" && r.Method == "GET":
		io.WriteString(w, `[]`)
	case p == "/movie/lookup/tmdb":
		io.WriteString(w, `{"title":"Arrival","year":2016,"tmdbId":329865,"images":[]}`)
	case p == "/series" && r.Method == "GET":
		io.WriteString(w, `[{"id":3,"title":"Severance","tmdbId":95396,"tvdbId":371980,"titleSlug":"severance","monitored":true,
			"seasons":[{"seasonNumber":0,"monitored":false},{"seasonNumber":1,"monitored":true},{"seasonNumber":2,"monitored":false}]}]`)
	case p == "/series/lookup":
		io.WriteString(w, `[{"title":"Andor","year":2022,"tmdbId":83867,"tvdbId":393189,"titleSlug":"andor",
			"seasons":[{"seasonNumber":0},{"seasonNumber":1},{"seasonNumber":2}]}]`)
	case p == "/episode":
		io.WriteString(w, `[{"id":51,"seasonNumber":1,"episodeNumber":1},{"id":52,"seasonNumber":1,"episodeNumber":2}]`)
	case p == "/history/series":
		io.WriteString(w, `[{"id":900,"episodeId":51},{"id":901,"episodeId":52}]`)
	case p == "/history/movie":
		io.WriteString(w, `[]`)
	case p == "/queue":
		io.WriteString(w, `{"records":[{"movieId":7,"size":1000,"sizeleft":250,"status":"downloading","trackedDownloadState":"downloading",
			"estimatedCompletionTime":"2099-01-01T00:10:00Z"}]}`)
	default:
		io.WriteString(w, `{}`)
	}
}

func (f *fakeArr) did(s string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.Contains(c, s) {
			return true
		}
	}
	return false
}

func TestSendAndTrack(t *testing.T) {
	radarr, sonarr := &fakeArr{kind: "radarr"}, &fakeArr{kind: "sonarr"}
	rs, ss := httptest.NewServer(radarr), httptest.NewServer(sonarr)
	defer rs.Close()
	defer ss.Close()
	a := &app{settings: map[string]string{"radarr_url": rs.URL, "radarr_key": "key", "radarr_profile": "hd-1080p",
		"sonarr_url": ss.URL, "sonarr_key": "key", "sonarr_root": "/tv"}}
	approved := []request{
		{ID: 1, Kind: "movie", TMDBID: 949, Title: "Heat"},                          // Radarr has it: searched
		{ID: 2, Kind: "movie", TMDBID: 329865, Title: "Arrival"},                    // added with a search
		{ID: 3, Kind: "show", TMDBID: 95396, Title: "Severance", Seasons: []int{2}}, // follows season 2 too
		{ID: 4, Kind: "show", TMDBID: 83867, Title: "Andor", Seasons: []int{1}},     // added, season 1 only
		{ID: 5, Kind: "movie", TMDBID: 1, Title: "Heat in 4K", FourK: true},         // no 4K Radarr: fails
	}
	reports := map[int64]map[string]any{}
	a.call = func(_ context.Context, method, path string, body, out any) error {
		switch {
		case method == "GET" && path == "/requests?status=approved":
			b, _ := json.Marshal(approved)
			return json.Unmarshal(b, out)
		case method == "GET" && path == "/requests?status=sent":
			b, _ := json.Marshal(approved[:1])
			return json.Unmarshal(b, out)
		case method == "PUT":
			b, _ := json.Marshal(body)
			var m map[string]any
			json.Unmarshal(b, &m)
			var id int64
			json.Unmarshal([]byte(path[strings.LastIndex(path, "/")+1:]), &id)
			reports[id] = m
		}
		return nil
	}
	a.sendRequests(context.Background())
	if !radarr.did(`POST /api/v3/command {"movieIds":[7],"name":"MoviesSearch"}`) {
		t.Errorf("Heat not searched: %v", radarr.calls)
	}
	if !radarr.did(`"addOptions":{"searchForMovie":true}`) || !radarr.did(`"qualityProfileId":4`) || !radarr.did(`"rootFolderPath":"/data/media"`) {
		t.Errorf("Arrival not added right: %v", radarr.calls)
	}
	if !sonarr.did(`PUT /api/v3/series/3`) || !sonarr.did(`{"monitored":true,"seasonNumber":2}`) ||
		!sonarr.did(`POST /api/v3/command {"name":"SeasonSearch","seasonNumber":2,"seriesId":3}`) {
		t.Errorf("Severance season 2: %v", sonarr.calls)
	}
	if !sonarr.did(`POST /api/v3/series`) || !sonarr.did(`"rootFolderPath":"/tv"`) || !sonarr.did(`"tvdbId":393189`) {
		t.Errorf("Andor not added: %v", sonarr.calls)
	}
	for id := int64(1); id <= 4; id++ {
		if reports[id]["status"] != "sent" {
			t.Errorf("request %d: %v", id, reports[id])
		}
	}
	if reports[3]["link"] != ss.URL+"/series/severance" {
		t.Errorf("Severance link: %v", reports[3])
	}
	if reports[5]["status"] != "failed" || !strings.Contains(reports[5]["reason"].(string), "4K Radarr") {
		t.Errorf("4K without a 4K Radarr: %v", reports[5])
	}

	// Heat is downloading: 75%, with Radarr's ETA.
	a.trackRequests(context.Background())
	if r := reports[1]; r["stage"] != "downloading" || r["progress"] != 0.75 || r["eta"].(float64) != float64(time.Date(2099, 1, 1, 0, 10, 0, 0, time.UTC).Unix()) {
		t.Errorf("progress: %v", r)
	}

	// A problem with an episode marks its grab failed.
	ep, season := 2, 1
	data, _ := json.Marshal(problem{ID: 8, ItemKind: "episode", TMDBID: 95396, Season: &season, Episode: &ep})
	a.refetch(context.Background(), data)
	if !sonarr.did("POST /api/v3/history/failed/901") || reports[8]["status"] != "resolved" {
		t.Errorf("refetch episode: %v %v", sonarr.calls, reports[8])
	}
	// A movie with no grab (added by hand) is searched instead.
	data, _ = json.Marshal(problem{ID: 9, ItemKind: "movie", TMDBID: 949})
	a.refetch(context.Background(), data)
	if !strings.Contains(reports[9]["reply"].(string), "searching") {
		t.Errorf("refetch movie: %v", reports[9])
	}
}
