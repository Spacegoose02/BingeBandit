package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

func TestWatchTogether(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	host, _ := st.CreateUser("host", string(hash), false)
	guest, _ := st.CreateUser("guest", string(hash), false)
	outsider, _ := st.CreateUser("outsider", string(hash), false)
	for tok, u := range map[string]int64{"host": host.ID, "guest": guest.ID, "outsider": outsider.ID} {
		st.CreateSession(tok+"-token", u, "Web", "")
	}
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	movie, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heist", MatchKey: "movie:heist"})
	other, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Sequel", MatchKey: "movie:sequel"})
	st.SetUserLibraries(host.ID, false, []int64{lib.ID})
	st.SetUserLibraries(guest.ID, false, []int64{lib.ID})
	st.SetUserLibraries(outsider.ID, false, []int64{}) // can't see the movie
	st.SetAcceptsRecommendations(guest.ID, true)

	srv := httptest.NewServer((&Server{Store: st, DataDir: dir}).Router(context.Background()))
	defer srv.Close()
	call := func(token, method, path, body string) (int, map[string]any) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	code, room := call("host-token", "POST", "/api/together", `{"itemId":`+itoa(movie)+`,"position":42,"paused":true}`)
	if code != http.StatusCreated {
		t.Fatalf("start: %d %v", code, room)
	}
	id := room["id"].(string)
	if code, _ := call("outsider-token", "GET", "/api/together/"+id, ""); code != http.StatusNotFound {
		t.Errorf("someone who can't see the movie got the room: %d", code)
	}

	// Invite: only people who opted in and can see the movie.
	if code, _ := call("host-token", "POST", "/api/together/"+id+"/invite", `{"userIds":[`+itoa(outsider.ID)+`]}`); code != http.StatusBadRequest {
		t.Errorf("inviting someone who isn't allowed: %d", code)
	}
	if code, _ := call("host-token", "POST", "/api/together/"+id+"/invite", `{"userIds":[`+itoa(guest.ID)+`]}`); code != http.StatusNoContent {
		t.Errorf("invite: %d", code)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/together/invites", nil)
	req.Header.Set("Authorization", "Bearer guest-token")
	res, _ := http.DefaultClient.Do(req)
	var invites []map[string]any
	json.NewDecoder(res.Body).Decode(&invites)
	res.Body.Close()
	if len(invites) != 1 || invites[0]["title"] != "Heist" || invites[0]["from"] != "host" {
		t.Fatalf("invites = %v", invites)
	}

	// The guest connects and hears the host's changes.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/together/"+id+"/events", nil)
	req.Header.Set("Authorization", "Bearer guest-token")
	res, err = http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("events: %v %v", err, res)
	}
	events := make(chan map[string]any, 10)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var m map[string]any
				json.Unmarshal([]byte(line), &m)
				events <- m
			}
		}
	}()
	next := func() map[string]any {
		select {
		case m := <-events:
			return m
		case <-time.After(3 * time.Second):
			t.Fatal("no event")
			return nil
		}
	}
	if m := next(); len(m["members"].([]any)) != 1 {
		t.Errorf("joining should list the guest: %v", m)
	}
	call("host-token", "POST", "/api/together/"+id+"/state", `{"paused":false,"position":100}`)
	m := next()
	stt := m["state"].(map[string]any)
	if stt["paused"] != false || stt["position"].(float64) != 100 || stt["byName"] != "host" || stt["rev"].(float64) != 2 {
		t.Errorf("state = %v", stt)
	}
	// Moving on to another movie; the invite is gone now that the guest joined.
	call("host-token", "POST", "/api/together/"+id+"/state", `{"itemId":`+itoa(other)+`,"paused":false,"position":0}`)
	if stt := next()["state"].(map[string]any); stt["itemId"].(float64) != float64(other) {
		t.Errorf("item change = %v", stt)
	}
	res2, _ := http.DefaultClient.Do(func() *http.Request {
		r, _ := http.NewRequest("GET", srv.URL+"/api/together/invites", nil)
		r.Header.Set("Authorization", "Bearer guest-token")
		return r
	}())
	var left []any
	json.NewDecoder(res2.Body).Decode(&left)
	res2.Body.Close()
	if len(left) != 0 {
		t.Errorf("invite should clear on joining: %v", left)
	}
	if code, _ := call("host-token", "POST", "/api/together/nope/state", `{}`); code != http.StatusNotFound {
		t.Errorf("unknown room: %d", code)
	}
	cancel()
	res.Body.Close()
}
