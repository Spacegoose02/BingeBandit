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

	"bingebandit/pkg/bbplugin"
)

func ip(n int) *int { return &n }

func TestFind(t *testing.T) {
	files := []file{
		// A 720p movie with a 4K copy next to it: nothing to do.
		{FileID: 1, ItemID: 10, Kind: "movie", Title: "Heat", TMDBID: 949, Width: 1280, Height: 720, VideoCodec: "h264"},
		{FileID: 2, ItemID: 10, Kind: "movie", Title: "Heat", TMDBID: 949, Width: 3840, Height: 2160, VideoCodec: "hevc", HDR: true},
		// Widescreen 1080p is 1080p, however short.
		{FileID: 3, ItemID: 11, Kind: "movie", Title: "Arrival", TMDBID: 329865, Width: 1920, Height: 800, VideoCodec: "h264", Bitrate: 9e6},
		// DVD-era Xvid.
		{FileID: 4, ItemID: 12, Kind: "movie", Title: "amélie", Year: 2001, Width: 720, Height: 400, VideoCodec: "mpeg4", Bitrate: 1.2e6},
		// Not probed yet: left alone.
		{FileID: 5, ItemID: 13, Kind: "movie", Title: "Unknown"},
		{FileID: 6, ItemID: 20, Kind: "episode", Title: "Pilot", ShowID: 5, ShowTitle: "Severance", ShowTMDBID: 95396, Season: ip(1), Episode: ip(2), Width: 1280, Height: 720, VideoCodec: "h264"},
		{FileID: 7, ItemID: 21, Kind: "episode", Title: "Good News", ShowID: 5, ShowTitle: "Severance", ShowTMDBID: 95396, Season: ip(1), Episode: ip(1), Width: 1280, Height: 720, VideoCodec: "h264"},
		{FileID: 8, ItemID: 22, Kind: "episode", Title: "Half Loop", ShowID: 5, ShowTitle: "Severance", ShowTMDBID: 95396, Season: ip(1), Episode: ip(3), Width: 1920, Height: 1080, VideoCodec: "hevc"},
		{FileID: 9, ItemID: 30, Kind: "episode", Title: "x", ShowID: 6, ShowTitle: "Fine Show", Season: ip(1), Episode: ip(1), Width: 1920, Height: 1080, VideoCodec: "h264"},
	}
	rep := find(files, rulesFrom(map[string]string{"target": "1080p", "old_codecs": "true", "min_bitrate": "2"}))
	if len(rep.Movies) != 1 || rep.Movies[0].Title != "amélie" {
		t.Fatalf("movies: %+v", rep.Movies)
	}
	if got := strings.Join(rep.Movies[0].Reasons, "; "); got != "SD, below 1080p; old codec (mpeg4); low bitrate (1.2 Mbps)" {
		t.Errorf("reasons: %s", got)
	}
	if len(rep.Shows) != 1 || rep.Shows[0].Total != 3 || len(rep.Shows[0].Episodes) != 2 || rep.Shows[0].Episodes[0].Episode != 1 {
		t.Fatalf("shows: %+v", rep.Shows)
	}
	// Aiming for 4K, the 1080p films join in; HDR only when asked.
	rep = find(files, rulesFrom(map[string]string{"target": "2160p", "old_codecs": "false"}))
	if len(rep.Movies) != 2 {
		t.Errorf("4K target: %+v", rep.Movies)
	}
	if r := rulesFrom(map[string]string{"want_hdr": "true"}); len(r.reasons(file{Width: 3840, Height: 2160, VideoCodec: "hevc"})) != 1 {
		t.Error("4K without HDR should be listed when asked")
	}
}

// fakeCinemask answers like Cinemask: movie 949 is monitored with the
// HD-720p profile, everything else isn't yet.
type fakeCinemask struct {
	mu       sync.Mutex
	calls    []string
	profile  int64
	searches []string
}

func (f *fakeCinemask) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-Api-Key") != "cmk_test" {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"Unauthorized"}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI()+" "+string(body))
	switch {
	case r.URL.Path == "/api/auth/me":
		io.WriteString(w, `{"user":{"username":"michael","role":"admin"}}`)
	case r.URL.Path == "/api/profiles":
		io.WriteString(w, `[{"id":1,"name":"HD-720p","cutoff":"Bluray-720p","upgradesAllowed":true},{"id":2,"name":"HD-1080p","cutoff":"Bluray-1080p","upgradesAllowed":true}]`)
	case r.URL.Path == "/api/monitored/lookup/movie/949":
		json.NewEncoder(w).Encode(map[string]any{"monitored": map[string]any{"id": 4, "title": "Heat", "monitored": true, "profileId": f.profile,
			"profile": map[string]any{"id": f.profile, "name": "HD-720p", "cutoff": "Bluray-720p"}, "fileQuality": "HDTV-720p"}})
	case strings.HasPrefix(r.URL.Path, "/api/monitored/lookup/"):
		io.WriteString(w, `{"monitored":null,"request":null}`)
	case r.Method == "PATCH" && r.URL.Path == "/api/monitored/4":
		var b struct{ ProfileID int64 }
		json.Unmarshal(body, &b)
		f.profile = b.ProfileID
		io.WriteString(w, `{}`)
	case r.Method == "POST" && r.URL.Path == "/api/monitored/4/search":
		f.searches = append(f.searches, string(body))
		if f.profile == 1 {
			io.WriteString(w, `{"grabbed":0,"message":"Nothing to search for: Heat already has a file at the profile's cutoff.","errors":[]}`)
		} else {
			io.WriteString(w, `{"grabbed":1,"errors":[]}`)
		}
	case r.Method == "GET" && r.URL.Path == "/api/monitored/4/releases":
		io.WriteString(w, `{"errors":[],"releases":[{"guid":"g1","title":"Heat.1995.1080p.BluRay","indexer":"x","size":9000000000,"quality":"Bluray-1080p","accepted":true,"rejections":[],"grabbed":null}]}`)
	case r.Method == "POST" && r.URL.Path == "/api/monitored/4/grab":
		io.WriteString(w, `{"downloadId":7}`)
	case r.Method == "POST" && r.URL.Path == "/api/monitored":
		io.WriteString(w, `{"id":9,"title":"Arrival","profileId":2}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":"Not found"}`)
	}
}

func TestRoutes(t *testing.T) {
	cm := &fakeCinemask{profile: 1}
	cs := httptest.NewServer(cm)
	defer cs.Close()
	a := &app{p: &bbplugin.Plugin{Manifest: manifest}}
	a.settings = map[string]string{"url": cs.URL, "api_key": "cmk_test", "target": "1080p", "old_codecs": "true", "min_bitrate": "0"}
	a.files = func(context.Context) ([]file, error) {
		return []file{
			{FileID: 1, ItemID: 10, Kind: "movie", Title: "Heat", TMDBID: 949, Width: 1280, Height: 720, VideoCodec: "h264"},
			{FileID: 2, ItemID: 11, Kind: "movie", Title: "Arrival", TMDBID: 329865, Width: 1280, Height: 536, VideoCodec: "h264"},
		}, nil
	}
	h := a.routes()
	do := func(method, path, body string, admin bool) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("X-BingeBandit-User-Id", "1")
		req.Header.Set("X-BingeBandit-User", "michael")
		req.Header.Set("X-BingeBandit-Admin", map[bool]string{true: "true", false: "false"}[admin])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}

	if code, _ := do("GET", "/report", "", false); code != http.StatusForbidden {
		t.Errorf("non-admin: %d", code)
	}
	code, body := do("GET", "/report", "", true)
	if code != 200 || !strings.Contains(body, `"ok":true`) || !strings.Contains(body, `"title":"Arrival"`) || !strings.Contains(body, `"title":"Heat"`) {
		t.Fatalf("report: %d %s", code, body)
	}

	// Heat is already at its 720p profile's cutoff, so Cinemask won't look.
	if code, body := do("POST", "/search", `{"kind":"movie","tmdbId":949}`, true); code != 200 || !strings.Contains(body, "cutoff") {
		t.Errorf("search at cutoff: %d %s", code, body)
	}
	// Moving it to HD-1080p first makes it worth searching.
	if code, body := do("POST", "/search", `{"kind":"movie","tmdbId":949,"profileId":2}`, true); code != 200 || !strings.Contains(body, `"grabbed":1`) || cm.profile != 2 {
		t.Errorf("search with a new profile: %d %s (profile %d)", code, body, cm.profile)
	}
	if code, body := do("POST", "/releases", `{"kind":"movie","tmdbId":949}`, true); code != 200 || !strings.Contains(body, `"guid":"g1"`) {
		t.Errorf("releases: %d %s", code, body)
	}
	if code, body := do("POST", "/grab", `{"kind":"movie","tmdbId":949,"guid":"g1"}`, true); code != http.StatusNoContent {
		t.Errorf("grab: %d %s", code, body)
	}

	// Arrival isn't in Cinemask: searching says so, adding never searches by itself.
	if code, body := do("POST", "/search", `{"kind":"movie","tmdbId":329865}`, true); code != http.StatusNotFound || !strings.Contains(body, "add it first") {
		t.Errorf("search unmonitored: %d %s", code, body)
	}
	if code, body := do("POST", "/monitor", `{"kind":"movie","tmdbId":329865,"profileId":2}`, true); code != 200 || !strings.Contains(body, `"id":9`) {
		t.Errorf("monitor: %d %s", code, body)
	}
	cm.mu.Lock()
	last := cm.calls[len(cm.calls)-1]
	cm.mu.Unlock()
	if !strings.Contains(last, `"search":false`) || !strings.Contains(last, `"profileId":2`) {
		t.Errorf("monitor call: %s", last)
	}

	// Bad input never reaches Cinemask.
	for _, b := range []string{`{"kind":"movie","tmdbId":0}`, `{"kind":"book","tmdbId":1}`, `{"kind":"movie","tmdbId":949,"season":1}`} {
		if code, _ := do("POST", "/search", b, true); code != http.StatusBadRequest {
			t.Errorf("%s: %d", b, code)
		}
	}

	// Search all: Heat is searched, Arrival (still unmonitored in the fake) is skipped.
	bulkPause = time.Millisecond
	if code, body := do("POST", "/search-all", "", true); code != http.StatusAccepted {
		t.Fatalf("search all: %d %s", code, body)
	}
	for i := 0; i < 100; i++ {
		a.mu.Lock()
		running := a.bulk.Running
		a.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.mu.Lock()
	b := a.bulk
	a.mu.Unlock()
	if b.Running || b.Done != 2 || b.Grabbed != 1 || b.Skipped != 1 || len(b.Errors) != 0 {
		t.Errorf("bulk: %+v", b)
	}

	// A wrong key is explained.
	a.settings["api_key"] = "nope"
	if _, body := do("GET", "/report", "", true); !strings.Contains(body, "didn't accept the API key") {
		t.Errorf("bad key: %s", body)
	}
}
