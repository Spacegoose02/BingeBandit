package livetv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestHDHomeRunSources(t *testing.T) {
	now := time.Now().Unix()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/discover.json":
			json.NewEncoder(w).Encode(map[string]any{"FriendlyName": "HDHomeRun FLEX 4K", "ModelNumber": "HDFX-4K", "DeviceID": "1053ABCD",
				"DeviceAuth": "auth123", "TunerCount": 4, "BaseURL": srv.URL, "LineupURL": srv.URL + "/lineup.json"})
		case "/lineup.json":
			io.WriteString(w, `[{"GuideNumber":"7.1","GuideName":"KABC-HD","URL":"`+srv.URL+`/auto/v7.1","HD":1},
				{"GuideNumber":"7.2","GuideName":"LAFF","URL":"`+srv.URL+`/auto/v7.2"},
				{"GuideNumber":"99","GuideName":"Locked","URL":"`+srv.URL+`/auto/v99","DRM":1}]`)
		case "/api/guide.php":
			if r.URL.Query().Get("DeviceAuth") != "auth123" {
				http.Error(w, "no", http.StatusForbidden)
				return
			}
			start := r.URL.Query().Get("Start")
			if start != "" && start > json.Number(itoa(now+3600)).String() {
				io.WriteString(w, `[]`) // the end of what the guide knows
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{{"GuideNumber": "7.1", "GuideName": "KABC-HD", "Guide": []map[string]any{
				{"StartTime": now - 600, "EndTime": now + 1200, "Title": "Eyewitness News", "Filter": []string{"News"}},
				{"StartTime": now + 1200, "EndTime": now + 7200, "Title": "Jeopardy!", "EpisodeNumber": "S41E12", "EpisodeTitle": "Champions"},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := hdhrGuideBase
	hdhrGuideBase = srv.URL
	defer func() { hdhrGuideBase = old }()

	st := openStore(t)
	addr := "hdhomerun://" + strings.TrimPrefix(srv.URL, "http://")
	pl := &store.LiveSource{Kind: "m3u", Name: "Antenna", URL: addr, Enabled: true}
	st.SaveLiveSource(pl)
	if err := Refresh(context.Background(), st, pl); err != nil {
		t.Fatal(err)
	}
	streams, total, _ := st.LiveStreams(store.StreamQuery{SourceID: pl.ID, Limit: 10})
	if total != 2 || streams[0].TvgID != "7.1" || streams[0].Name != "KABC-HD" || !strings.HasSuffix(streams[0].URL, "/auto/v7.1") || streams[0].Group != "Antenna" {
		t.Fatalf("streams: %d %+v", total, streams)
	}

	guide := &store.LiveSource{Kind: "epg", Name: "Antenna guide", URL: addr, Enabled: true}
	st.SaveLiveSource(guide)
	if err := Refresh(context.Background(), st, guide); err != nil {
		t.Fatal(err)
	}
	progs, err := st.Programs([]string{"7.1"}, now-3600, now+86400)
	if err != nil || len(progs) != 2 || progs[1].Title != "Jeopardy!" || progs[1].Episode != "S41E12" || progs[0].Category != "News" {
		t.Fatalf("programs: %+v %v", progs, err)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestSchedulesDirectGuide(t *testing.T) {
	day := time.Now().UTC().Format("2006-01-02")
	airAt := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/20141201/token" && r.Header.Get("token") != "tok" {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"message":"bad token"}`)
			return
		}
		switch r.URL.Path {
		case "/20141201/token":
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			// sha1("secret")
			if b["username"] != "dana" || b["password"] != "e5e9fa1ba31ecd1ae84f75caaa474f3a663f05f4" {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"message":"Invalid user"}`)
				return
			}
			io.WriteString(w, `{"token":"tok"}`)
		case "/20141201/lineups":
			io.WriteString(w, `{"lineups":[{"lineup":"USA-OTA-90210","uri":"/20141201/lineups/USA-OTA-90210"},{"lineup":"USA-CA66511-X","uri":"/20141201/lineups/USA-CA66511-X"}]}`)
		case "/20141201/lineups/USA-OTA-90210":
			io.WriteString(w, `{"map":[{"stationID":"19571","channel":"007.1","atscMajor":7,"atscMinor":1}],"stations":[{"stationID":"19571","callsign":"KABC","logo":{"URL":"https://logo/kabc.png"}}]}`)
		case "/20141201/lineups/USA-CA66511-X":
			io.WriteString(w, `{"map":[{"stationID":"10035","channel":"206"}],"stations":[{"stationID":"10035","name":"ESPN"}]}`)
		case "/20141201/schedules":
			var req []struct {
				StationID string   `json:"stationID"`
				Date      []string `json:"date"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if len(req) != 2 || len(req[0].Date) != SDDays || !strings.Contains(strings.Join(req[0].Date, ","), day) {
				t.Errorf("schedules request: %+v", req)
			}
			var out []map[string]any
			for _, s := range req {
				out = append(out, map[string]any{"stationID": s.StationID, "programs": []map[string]any{
					{"programID": "EP" + s.StationID, "airDateTime": airAt.Format(time.RFC3339), "duration": 1800}}})
			}
			json.NewEncoder(w).Encode(out)
		case "/20141201/programs":
			io.WriteString(w, `[{"programID":"EP19571","titles":[{"title120":"Jeopardy!"}],"episodeTitle150":"Champions",
				"descriptions":{"description1000":[{"description":"Long one."}]},"genres":["Game show"],
				"metadata":[{"Gracenote":{"season":41,"episode":12}}],"contentRating":[{"body":"USA Parental Rating","code":"TVG"}]},
				{"programID":"EP10035","titles":[{"title120":"SportsCenter"}]}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := sdBase
	sdBase = srv.URL + "/20141201"
	defer func() { sdBase = old }()

	st := openStore(t)
	src := &store.LiveSource{Kind: "epg", Name: "SD", URL: "schedulesdirect://dana:secret", Enabled: true}
	st.SaveLiveSource(src)
	if err := Refresh(context.Background(), st, src); err != nil {
		t.Fatal(err)
	}
	from, to := airAt.Add(-time.Hour).Unix(), airAt.Add(time.Hour).Unix()
	// The antenna lineup is named by channel number, cable by station id.
	progs, _ := st.Programs([]string{"7.1"}, from, to)
	if len(progs) != 1 || progs[0].Title != "Jeopardy!" || progs[0].Episode != "S41E12" || progs[0].Rating != "TVG" || progs[0].Description != "Long one." {
		t.Fatalf("antenna programs: %+v", progs)
	}
	if progs, _ := st.Programs([]string{"10035"}, from, to); len(progs) != 1 || progs[0].Title != "SportsCenter" {
		t.Fatalf("cable programs: %+v", progs)
	}
	chans, _ := st.EPGChannels("KABC", 10)
	if len(chans) != 1 || chans[0].ID != "7.1" || chans[0].Icon != "https://logo/kabc.png" {
		t.Fatalf("channels: %+v", chans)
	}

	bad := &store.LiveSource{Kind: "epg", Name: "SD", URL: "schedulesdirect://dana:wrong", Enabled: true}
	st.SaveLiveSource(bad)
	if err := Refresh(context.Background(), st, bad); err == nil || !strings.Contains(err.Error(), "Invalid user") {
		t.Fatalf("wrong password: %v", err)
	}
}
