package api

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

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
	"bingebandit/internal/stream"
	"bingebandit/internal/trakt"
)

func TestScreenTime(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("dana", string(hash), true)
	kid, _ := st.CreateUser("sam", string(hash), false)
	st.SetUserLibraries(kid.ID, true, nil)
	st.CreateSession("dana", admin.ID, "Web", "")
	st.CreateSession("sam", kid.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	item, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Alpha", MatchKey: "movie:Alpha"})

	// A Wednesday afternoon.
	now := time.Date(2026, 11, 4, 15, 0, 0, 0, time.Local)
	screenTimeNow = func() time.Time { return now }
	defer func() { screenTimeNow = time.Now }()

	h := (&Server{Store: st, Activity: stream.NewActivity(), Trakt: trakt.NewSyncer(st, "")}).Router(context.Background())
	call := func(token, method, path, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	progress := func() (int, map[string]any) {
		return call("sam", "POST", "/api/progress", fmt.Sprintf(`{"itemId":%d,"position":10,"duration":100,"playSessionId":"p1"}`, item))
	}
	rules := fmt.Sprintf("/api/admin/users/%d/screen-time", kid.ID)

	// No rules: nothing counts, nothing stops.
	if c, _ := progress(); c != http.StatusOK {
		t.Fatalf("progress without rules: %d", c)
	}
	if c, _ := call("dana", "PUT", rules, `{"weekdayMinutes":1,"bedtime":"20:00"}`); c != http.StatusBadRequest {
		t.Fatalf("bedtime without wake: %d", c)
	}
	if c, _ := call("dana", "PUT", rules, `{"weekdayMinutes":1,"weekendMinutes":30,"bedtime":"20:00","wake":"07:00"}`); c != http.StatusOK {
		t.Fatalf("set rules: %d", c)
	}
	// Admins have none.
	if c, _ := call("dana", "PUT", fmt.Sprintf("/api/admin/users/%d/screen-time", admin.ID), `{"weekdayMinutes":1}`); c != http.StatusBadRequest {
		t.Fatalf("rules for an admin: %d", c)
	}

	// The first report starts the clock; half a minute later is within the
	// minute, and the next uses it up.
	now = now.Add(30 * time.Second)
	if c, out := progress(); c != http.StatusOK {
		t.Fatalf("progress within the allowance: %d %v", c, out)
	}
	now = now.Add(30 * time.Second)
	c, out := progress()
	if c != http.StatusForbidden || !strings.HasPrefix(fmt.Sprint(out["error"]), "Screen time is up") {
		t.Fatalf("progress over the allowance: %d %v", c, out)
	}
	_, me := call("sam", "GET", "/api/me/screen-time", "")
	if me["leftMinutes"] != float64(0) || me["blocked"] == nil {
		t.Fatalf("my screen time: %v", me)
	}
	if c, _ := call("sam", "POST", "/api/live/channels/1/watch", ""); c != http.StatusForbidden && c != http.StatusNotFound {
		t.Fatalf("live tv: %d", c)
	}
	// A pause doesn't count and isn't refused.
	if c, _ := call("sam", "POST", "/api/progress", fmt.Sprintf(`{"itemId":%d,"position":10,"duration":100,"playSessionId":"p1","paused":true}`, item)); c != http.StatusOK {
		t.Fatalf("paused report: %d", c)
	}

	// More time today.
	call("dana", "PUT", rules, `{"weekdayMinutes":1,"weekendMinutes":30,"bedtime":"20:00","wake":"07:00","reset":true}`)
	if c, _ := progress(); c != http.StatusOK {
		t.Fatalf("after reset: %d", c)
	}

	// Bedtime, past midnight too.
	for _, at := range []time.Time{time.Date(2026, 11, 7, 21, 0, 0, 0, time.Local), time.Date(2026, 11, 8, 6, 30, 0, 0, time.Local)} {
		now = at
		c, out := progress()
		if c != http.StatusForbidden || !strings.Contains(fmt.Sprint(out["error"]), "opens again at 07:00") {
			t.Fatalf("bedtime at %s: %d %v", at.Format("15:04"), c, out)
		}
	}
	// Saturday daytime: the weekend allowance.
	now = time.Date(2026, 11, 7, 10, 0, 0, 0, time.Local)
	if c, _ := progress(); c != http.StatusOK {
		t.Fatalf("weekend: %d", c)
	}
	_, me = call("sam", "GET", "/api/me/screen-time", "")
	if me["leftMinutes"] != float64(30) {
		t.Fatalf("weekend allowance: %v", me)
	}
}
