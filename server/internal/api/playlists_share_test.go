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

func TestSharedPlaylists(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dana, _ := st.CreateUser("dana", "x", false)
	sam, _ := st.CreateUser("sam", "x", false)
	lee, _ := st.CreateUser("lee", "x", false) // doesn't take recommendations
	for _, u := range []*store.User{dana, sam, lee} {
		st.SetUserLibraries(u.ID, true, nil)
		st.CreateSession(u.Username+"-token", u.ID, "Web", "")
	}
	st.SetAcceptsRecommendations(sam.ID, true)
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	heat, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", MatchKey: "movie:heat"})
	h := (&Server{Store: st}).Router(context.Background())

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

	var p store.Playlist
	if c := call("dana-token", "POST", "/api/playlists", `{"name":"Friday","itemIds":[`+itoa(heat)+`]}`, &p); c != http.StatusCreated {
		t.Fatalf("create: %d", c)
	}
	pl := "/api/playlists/" + itoa(p.ID)
	if c := call("sam-token", "GET", pl, "", nil); c != http.StatusNotFound {
		t.Fatalf("not shared yet: %d", c)
	}

	var sharing struct {
		People     []store.Recipient `json:"people"`
		SharedWith []int64           `json:"sharedWith"`
	}
	call("dana-token", "GET", pl+"/share", "", &sharing)
	if len(sharing.People) != 1 || sharing.People[0].ID != sam.ID || len(sharing.SharedWith) != 0 {
		t.Fatalf("sharing = %+v", sharing)
	}
	if c := call("dana-token", "PUT", pl+"/share", `{"userIds":[`+itoa(lee.ID)+`]}`, nil); c != http.StatusBadRequest {
		t.Fatalf("share with someone who didn't opt in: %d", c)
	}
	if c := call("dana-token", "PUT", pl+"/share", `{"userIds":[`+itoa(sam.ID)+`]}`, nil); c != http.StatusNoContent {
		t.Fatalf("share: %d", c)
	}

	// Sam sees and plays it, but can't change or reshare it.
	var shared []store.Playlist
	call("sam-token", "GET", "/api/playlists/shared", "", &shared)
	if len(shared) != 1 || shared[0].Owner != "dana" || shared[0].ItemCount != 1 {
		t.Fatalf("shared with sam = %+v", shared)
	}
	var got struct {
		Playlist store.Playlist `json:"playlist"`
		Items    []store.Item   `json:"items"`
	}
	if c := call("sam-token", "GET", pl, "", &got); c != http.StatusOK || len(got.Items) != 1 || got.Playlist.Owner != "dana" {
		t.Fatalf("sam opens it: %d %+v", c, got)
	}
	for _, try := range [][3]string{{"PUT", pl, `{"name":"Mine"}`}, {"DELETE", pl, ""}, {"PUT", pl + "/share", `{"userIds":[]}`}} {
		if c := call("sam-token", try[0], try[1], try[2], nil); c != http.StatusNotFound {
			t.Fatalf("sam %s %s: %d", try[0], try[1], c)
		}
	}
	if c := call("lee-token", "GET", pl, "", nil); c != http.StatusNotFound {
		t.Fatalf("lee opens it: %d", c)
	}
	var own []store.Playlist
	call("dana-token", "GET", "/api/playlists", "", &own)
	if len(own) != 1 || own[0].Shares != 1 || own[0].Owner != "" {
		t.Fatalf("dana's playlists = %+v", own)
	}

	// Unsharing takes it away again.
	call("dana-token", "PUT", pl+"/share", `{"userIds":[]}`, nil)
	if c := call("sam-token", "GET", pl, "", nil); c != http.StatusNotFound {
		t.Fatalf("after unsharing: %d", c)
	}
}
