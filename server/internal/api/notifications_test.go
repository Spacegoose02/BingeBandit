package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

func TestNotifications(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	kid, _ := st.CreateUser("kid", string(hash), false)
	quiet, _ := st.CreateUser("quiet", string(hash), false)
	for tok, id := range map[string]int64{"dana": dana.ID, "sam": sam.ID, "kid": kid.ID, "quiet": quiet.ID} {
		st.CreateSession(tok, id, "Web", "")
	}
	tv := &store.Library{Name: "TV", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(tv)
	st.SetUserLibraries(sam.ID, true, nil)
	st.SetUserLibraries(quiet.ID, true, nil)
	st.SetUserLibraries(kid.ID, false, nil) // can't see TV
	show, _ := st.UpsertItem(&store.Item{LibraryID: tv.ID, Kind: "show", Title: "Severance", MatchKey: "show:severance"})
	season, _ := st.UpsertItem(&store.Item{LibraryID: tv.ID, Kind: "season", ParentID: show, ShowID: show, Title: "Season 1", MatchKey: "season:severance:1", SeasonNumber: ip(1)})
	episode := func(n int) int64 {
		id, _ := st.UpsertItem(&store.Item{LibraryID: tv.ID, Kind: "episode", ParentID: season, ShowID: show, Title: "Episode " + itoa(int64(n)),
			MatchKey: "ep:severance:1:" + itoa(int64(n)), SeasonNumber: ip(1), EpisodeNumber: ip(n)})
		return id
	}
	ep1 := episode(1)

	srv := &Server{Store: st}
	h := srv.Router(context.Background())
	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	type list struct {
		Notifications []store.Notification `json:"notifications"`
		Unread        int                  `json:"unread"`
	}

	// The first run only remembers where the library stands.
	srv.notifyNewEpisodes()
	if st.Setting(settingNotifyWatermark) == "" {
		t.Fatal("watermark not set")
	}

	// Sam watched episode 1, Dana and the kid have the show on their
	// watchlist, Quiet watched it but turned notifications off.
	st.SetWatched(sam.ID, []int64{ep1}, true)
	st.SetWatched(quiet.ID, []int64{ep1}, true)
	st.AddToWatchlist(dana.ID, show)
	st.AddToWatchlist(kid.ID, show)
	if c := call("quiet", "PUT", "/api/me/prefs", `{"notifyEpisodes":false}`, nil); c != http.StatusOK && c != http.StatusNoContent {
		t.Fatalf("prefs: %d", c)
	}
	episode(2)
	episode(3)
	st.SetSettings(map[string]string{settingNotifyWatermark: "0"}) // everything counts as new, episode 1 included
	srv.notifyNewEpisodes()

	var got list
	call("sam", "GET", "/api/notifications", "", &got)
	if got.Unread != 1 || len(got.Notifications) != 1 || got.Notifications[0].Title != "2 new episodes of Severance" ||
		got.Notifications[0].Body != "S01E02 · Episode 2 to S01E03 · Episode 3" {
		t.Fatalf("sam: %+v", got)
	}
	call("dana", "GET", "/api/notifications", "", &got)
	if got.Unread != 1 || got.Notifications[0].Title != "3 new episodes of Severance" || got.Notifications[0].Link != "/item/"+itoa(show) {
		t.Fatalf("dana: %+v", got)
	}
	for _, tok := range []string{"kid", "quiet"} {
		call(tok, "GET", "/api/notifications", "", &got)
		if len(got.Notifications) != 0 {
			t.Fatalf("%s should get nothing: %+v", tok, got)
		}
	}
	// Running again finds nothing new.
	srv.notifyNewEpisodes()
	call("sam", "GET", "/api/notifications", "", &got)
	if len(got.Notifications) != 1 {
		t.Fatalf("repeat run: %+v", got)
	}

	// Recordings notify their owner.
	srv.Emit("recording.completed", &store.Recording{UserID: sam.ID, Title: "The News", ChannelName: "BBC One"})
	srv.Emit("recording.failed", &store.Recording{UserID: sam.ID, Title: "Match of the Day", Error: "tuner busy"})
	srv.Emit("recording.started", &store.Recording{UserID: sam.ID, Title: "Ignored"})
	call("sam", "GET", "/api/notifications", "", &got)
	if got.Unread != 3 || got.Notifications[0].Title != "Recording failed: Match of the Day" || got.Notifications[0].Body != "tuner busy" ||
		got.Notifications[1].Title != "Recorded The News" {
		t.Fatalf("recordings: %+v", got)
	}

	// Mark one read, then the rest; others can't touch Sam's.
	first := got.Notifications[0].ID
	call("dana", "POST", "/api/notifications/read", `{"ids":[`+itoa(first)+`]}`, nil)
	call("sam", "GET", "/api/notifications", "", &got)
	if got.Unread != 3 {
		t.Fatalf("dana marked sam's: %+v", got.Unread)
	}
	if c := call("sam", "POST", "/api/notifications/read", `{"ids":[`+itoa(first)+`]}`, nil); c != http.StatusNoContent {
		t.Fatalf("read: %d", c)
	}
	call("sam", "GET", "/api/notifications", "", &got)
	if got.Unread != 2 || got.Notifications[0].ReadAt == 0 {
		t.Fatalf("after read one: %+v", got)
	}
	call("sam", "POST", "/api/notifications/read", "", nil)
	call("sam", "GET", "/api/notifications", "", &got)
	if got.Unread != 0 {
		t.Fatalf("after read all: %+v", got.Unread)
	}
	if c := call("sam", "DELETE", "/api/notifications", "", nil); c != http.StatusNoContent {
		t.Fatalf("clear: %d", c)
	}
	call("sam", "GET", "/api/notifications", "", &got)
	if len(got.Notifications) != 0 {
		t.Fatalf("after clear: %+v", got)
	}
}

func ip(n int) *int { return &n }
