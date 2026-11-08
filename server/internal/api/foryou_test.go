package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

func TestRecommendationsFromTheLibrary(t *testing.T) {
	// A fake TMDB: everything is like everything, newest id first.
	tmdb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/recommendations") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"results":[{"id":199,"title":"Not here"},{"id":103,"title":"Gamma"},{"id":102,"title":"Beta"},{"id":101,"title":"Alpha"},{"id":104,"title":"Delta"},{"id":105,"title":"Epsilon"}]}`))
	}))
	defer tmdb.Close()
	defer metadata.SetTMDBBase(tmdb.URL)()

	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingTMDBKey: "fake-key"})
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	st.CreateSession("dana", dana.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	ids := map[string]int64{}
	for i, name := range []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"} {
		id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: name, MatchKey: "movie:" + name})
		genres := []string{"Drama"}
		if name == "Zeta" {
			genres = []string{"Drama", "Comedy"}
		}
		st.UpdateMetadata(id, &store.Item{Title: name, TMDBID: int64(101 + i), Genres: genres})
		ids[name] = id
	}
	// Dana finished Alpha.
	now := time.Now().Unix()
	st.StartPlay(&store.Play{UserID: dana.ID, ItemID: ids["Alpha"], Title: "Alpha", Kind: "movie", LibraryID: lib.ID, StartedAt: now, StoppedAt: now, Completed: true})
	st.SetWatched(dana.ID, []int64{ids["Alpha"]}, true)

	h := (&Server{Store: st, Metadata: metadata.NewService(st)}).Router(context.Background())
	call := func(method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer dana")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	titles := func(items []*store.Item) string {
		var out []string
		for _, it := range items {
			out = append(out, it.Title)
		}
		return strings.Join(out, ",")
	}
	type home struct {
		Rows []struct {
			ID    string        `json:"id"`
			Title string        `json:"title"`
			Items []*store.Item `json:"items"`
		} `json:"rows"`
	}
	because := func() string {
		var hm home
		call("GET", "/api/home", "", &hm)
		for _, r := range hm.Rows {
			if strings.HasPrefix(r.ID, "because-") {
				return r.Title + ": " + titles(r.Items)
			}
		}
		return ""
	}

	// Only what the server has, in TMDB's order, without Alpha itself.
	if got := because(); got != "Because you watched Alpha: Gamma,Beta,Delta,Epsilon" {
		t.Fatalf("because row: %q", got)
	}

	// More like this: TMDB's first, then genre matches (Zeta).
	var similar []*store.Item
	call("GET", "/api/items/"+itoa(ids["Beta"])+"/similar", "", &similar)
	if got := titles(similar); got != "Gamma,Alpha,Delta,Epsilon,Zeta" {
		t.Fatalf("similar: %q", got)
	}

	// A thumbs down leaves Gamma out of both.
	if c := call("PUT", "/api/items/"+itoa(ids["Gamma"])+"/rating", `{"rating":-1}`, nil); c != http.StatusOK {
		t.Fatalf("rate: %d", c)
	}
	if got := because(); got != "Because you watched Alpha: Beta,Delta,Epsilon" {
		t.Fatalf("because row after thumbs down: %q", got)
	}
	call("GET", "/api/items/"+itoa(ids["Beta"])+"/similar", "", &similar)
	if strings.Contains(titles(similar), "Gamma") {
		t.Fatalf("disliked title still similar: %s", titles(similar))
	}
	var detail map[string]any
	call("GET", "/api/items/"+itoa(ids["Gamma"]), "", &detail)
	if detail["myRating"] != float64(-1) {
		t.Fatalf("myRating: %v", detail["myRating"])
	}
	if c := call("PUT", "/api/items/"+itoa(ids["Gamma"])+"/rating", `{"rating":3}`, nil); c != http.StatusBadRequest {
		t.Fatalf("bad rating: %d", c)
	}

	// A thumbs up adds a "Because you liked" row.
	call("PUT", "/api/items/"+itoa(ids["Zeta"])+"/rating", `{"rating":2}`, nil)
	var hm home
	call("GET", "/api/home", "", &hm)
	found := false
	for _, r := range hm.Rows {
		found = found || r.Title == "Because you liked Zeta"
	}
	if !found {
		t.Fatal("no Because you liked row")
	}

	// The admin's switch takes the rows away.
	st.SetSettings(map[string]string{store.SettingBecauseRows: "false"})
	if got := because(); got != "" {
		t.Fatalf("rows with the switch off: %q", got)
	}
}
