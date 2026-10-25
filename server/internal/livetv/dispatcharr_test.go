package livetv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

// fakeDispatcharr serves a small Dispatcharr API plus the providers it uses:
// an Xtream account (whose playlist builds URLs differently from
// Dispatcharr), a plain M3U account, and an XMLTV guide.
func fakeDispatcharr(t *testing.T) *httptest.Server {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := ts.URL
		authed := r.Header.Get("Authorization") == "Bearer tok"
		send := func(v string) { w.Header().Set("Content-Type", "application/json"); fmt.Fprint(w, v) }
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/accounts/token/" && !authed {
			http.Error(w, `{"detail":"no"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/accounts/token/":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "admin" || body["password"] != "pw" {
				http.Error(w, `{"detail":"bad"}`, http.StatusUnauthorized)
				return
			}
			send(`{"access":"tok","refresh":"r"}`)
		case "/api/m3u/accounts/":
			send(fmt.Sprintf(`[
				{"id":1,"name":"Provider A","server_url":%q,"account_type":"XC","username":"u","password":"p","max_streams":2,"is_active":true,
				 "user_agent":1,"refresh_interval":6,"auto_enable_new_groups_live":false,
				 "channel_groups":[{"channel_group":10,"enabled":true},{"channel_group":11,"enabled":false},{"channel_group":12,"enabled":false}]},
				{"id":2,"name":"Provider B","server_url":%q,"account_type":"STD","max_streams":0,"is_active":true,"user_agent":null,"channel_groups":[]},
				{"id":3,"name":"custom","locked":true,"server_url":null,"account_type":"STD","is_active":true,"channel_groups":[]}]`,
				u+"/xc/", u+"/b.m3u"))
		case "/api/core/useragents/":
			send(`[{"id":1,"name":"TiviMate","user_agent":"TiviMate/5"}]`)
		case "/api/epg/sources/":
			send(fmt.Sprintf(`[{"id":1,"name":"Guide","source_type":"xmltv","url":%q,"is_active":true,"refresh_interval":0},
				{"id":2,"name":"Dummy","source_type":"dummy","url":null,"is_active":true}]`, u+"/epg.xml"))
		case "/api/channels/groups/":
			send(`[{"id":10,"name":"Sports"},{"id":11,"name":"News"},{"id":12,"name":"Kids"},{"id":20,"name":"My Sports"}]`)
		case "/api/channels/logos/":
			send(`{"count":2,"next":null,"results":[{"id":1,"name":"ESPN","url":"http://logos/espn.png"},{"id":2,"name":"CNN","url":"/data/logos/cnn.png"}]}`)
		case "/api/channels/channels/":
			if r.URL.Query().Get("page") == "2" {
				send(fmt.Sprintf(`{"count":3,"next":null,"results":[
					{"id":3,"channel_number":7,"name":"Gone","streams":[{"id":9,"name":"Gone","url":%q,"m3u_account":2,"channel_group":null}]}]}`, u+"/gone"))
				return
			}
			send(fmt.Sprintf(`{"count":3,"next":"http://internal-name:9191/api/channels/channels/?include_streams=true&page=2","results":[
				{"id":1,"channel_number":5,"name":"ESPN raw","effective_name":"ESPN","channel_group_id":20,"tvg_id":"x","epg_data_id":7,"logo_id":1,
				 "streams":[{"id":1,"name":"ESPN HD","url":%q,"m3u_account":1,"channel_group":10,"stream_id":101},
				            {"id":3,"name":"ESPN","url":%q,"m3u_account":2,"channel_group":null}]},
				{"id":2,"channel_number":6,"name":"CNN","channel_group_id":11,"tvg_id":"cnn.us","logo_id":2,"hidden_from_output":true,
				 "streams":[{"id":2,"name":"CNN","url":%q,"m3u_account":1,"channel_group":11,"stream_id":102}]}]}`,
				u+"/live/u/p/101.ts", u+"/b/espn", u+"/live/u/p/102.ts"))
		case "/api/epg/epgdata/":
			send(`[{"id":7,"tvg_id":"espn.us","name":"ESPN"}]`)
		case "/xc/get.php":
			if r.URL.Query().Get("username") != "u" || r.URL.Query().Get("password") != "p" || r.Header.Get("User-Agent") != "TiviMate/5" {
				http.Error(w, "no", http.StatusForbidden)
				return
			}
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1 group-title=\"Sports\",ESPN HD\n%[1]s/u/p/101\n#EXTINF:-1 group-title=\"News\",CNN\n%[1]s/u/p/102\n#EXTINF:-1 group-title=\"Kids\",Toons\n%[1]s/u/p/103\n", u)
		case "/b.m3u":
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1,ESPN\n%s/b/espn\n", u)
		case "/epg.xml":
			fmt.Fprint(w, `<tv><channel id="espn.us"><display-name>ESPN</display-name></channel></tv>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func waitDispatcharr(t *testing.T, j *DispatcharrJob) DispatcharrStatus {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := j.Status(); !s.Running {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("import didn't finish")
	return DispatcharrStatus{}
}

func TestDispatcharrImport(t *testing.T) {
	ts := fakeDispatcharr(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := NewDispatcharr(ts.URL, "admin", "wrong", "").Fetch(ctx); err == nil || !strings.Contains(err.Error(), "username and password") {
		t.Fatalf("bad login: %v", err)
	}
	setup, err := NewDispatcharr(ts.URL+"/", "admin", "pw", "").Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan := setup.Plan()
	if len(plan.Accounts) != 2 || len(plan.Guides) != 2 || plan.Channels != 3 || plan.Guides[1].Skip == "" {
		t.Fatalf("plan: %+v", plan)
	}

	var job DispatcharrJob
	if err := job.Start(ctx, st, setup); err != nil {
		t.Fatal(err)
	}
	res := waitDispatcharr(t, &job)
	if res.Error != "" || res.Sources != 3 || res.Channels != 2 {
		t.Fatalf("result: %+v", res)
	}
	if len(res.Notes) != 2 || !strings.Contains(res.Notes[0], "Dummy") || !strings.Contains(res.Notes[1], "Gone") {
		t.Fatalf("notes: %q", res.Notes)
	}

	srcs, _ := st.LiveSources("m3u")
	var a *store.LiveSource
	for _, s := range srcs {
		if s.Name == "Provider A" {
			a = s
		}
	}
	if a == nil || a.MaxStreams != 2 || a.UserAgent != "TiviMate/5" || a.RefreshHours != 6 || !strings.Contains(a.URL, "/xc/get.php?") {
		t.Fatalf("provider A: %+v", a)
	}
	groups, _ := st.SourceGroups(a.ID)
	on := map[string]bool{}
	for _, g := range groups {
		on[g.Name] = g.Enabled
	}
	// News is off in Dispatcharr but a channel uses it; Kids stays off.
	if !on["Sports"] || !on["News"] || on["Kids"] {
		t.Fatalf("groups: %+v", groups)
	}

	chans, _ := st.Channels(0, true)
	if len(chans) != 2 {
		t.Fatalf("channels: %d", len(chans))
	}
	espn, cnn := chans[0], chans[1]
	if espn.Number != 5 || espn.Name != "ESPN" || espn.Group != "My Sports" || espn.EPGID != "espn.us" || espn.Logo != "http://logos/espn.png" || !espn.Enabled {
		t.Fatalf("espn: %+v", espn)
	}
	if len(espn.Streams) != 2 || espn.Streams[0].URL != ts.URL+"/u/p/101" || espn.Streams[1].URL != ts.URL+"/b/espn" {
		t.Fatalf("espn streams: %+v", espn.Streams)
	}
	if cnn.Number != 6 || cnn.Enabled || cnn.Logo != ts.URL+"/api/channels/logos/2/cache/" || cnn.EPGID != "cnn.us" || len(cnn.Streams) != 1 {
		t.Fatalf("cnn: %+v", cnn)
	}

	// Importing again adds nothing.
	setup, _ = NewDispatcharr(ts.URL, "admin", "pw", "").Fetch(ctx)
	job.Start(ctx, st, setup)
	res = waitDispatcharr(t, &job)
	if res.Error != "" || res.Sources != 0 || res.Channels != 0 || res.Existing != 2 {
		t.Fatalf("second import: %+v", res)
	}
}

func TestXCStreamID(t *testing.T) {
	for in, want := range map[string]string{
		"http://h/live/u/p/123.ts": "123",
		"http://h/u/p/123":         "123",
		"http://h/live/u/p/x.ts":   "",
		"http://h/123.ts":          "",
	} {
		if got := xcStreamID(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
