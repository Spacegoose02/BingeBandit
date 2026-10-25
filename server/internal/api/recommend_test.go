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

func TestRecommendations(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	dana, _ := st.CreateUser("dana", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	kid, _ := st.CreateUser("kid", string(hash), false)
	shy, _ := st.CreateUser("shy", string(hash), false)
	for tok, id := range map[string]int64{"dana": dana.ID, "sam": sam.ID, "kid": kid.ID, "shy": shy.ID} {
		st.CreateSession(tok, id, "Web", "")
	}
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	movie, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Parasite", Year: 2019, MatchKey: "movie:parasite:2019"})
	st.SetUserLibraries(sam.ID, true, nil)
	st.SetUserLibraries(shy.ID, true, nil)
	st.SetUserLibraries(kid.ID, false, nil) // can't see Movies
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
	item := "/api/items/" + itoa(movie)

	// Nobody opted in yet.
	var rcpts []store.Recipient
	call("dana", "GET", item+"/recommend", "", &rcpts)
	if len(rcpts) != 0 {
		t.Fatalf("recipients before opting in: %+v", rcpts)
	}
	for _, tok := range []string{"sam", "kid"} {
		if c := call(tok, "PUT", "/api/me/recommendations", `{"accept":true}`, nil); c != http.StatusNoContent {
			t.Fatalf("opt in: %d", c)
		}
	}
	var opt map[string]bool
	call("sam", "GET", "/api/me/recommendations", "", &opt)
	if !opt["accept"] {
		t.Fatal("opt-in not saved")
	}
	// The kid can't see Movies and shy never opted in: only Sam is offered.
	call("dana", "GET", item+"/recommend", "", &rcpts)
	if len(rcpts) != 1 || rcpts[0].ID != sam.ID {
		t.Fatalf("recipients: %+v", rcpts)
	}
	var sent map[string]int
	body := `{"userIds":[` + itoa(sam.ID) + `,` + itoa(kid.ID) + `,` + itoa(shy.ID) + `],"note":"You'll love the ending"}`
	if c := call("dana", "POST", item+"/recommend", body, &sent); c != http.StatusOK || sent["sent"] != 1 {
		t.Fatalf("recommend: %d %v", c, sent)
	}

	var home struct {
		Rows []struct {
			ID    string        `json:"id"`
			Items []*store.Item `json:"items"`
		} `json:"rows"`
		RecommendedBy map[string]store.Recommendation `json:"recommendedBy"`
	}
	recommendedIn := func(tok string) int {
		home.Rows, home.RecommendedBy = nil, nil
		call(tok, "GET", "/api/home", "", &home)
		for _, r := range home.Rows {
			if r.ID == "recommended" {
				return len(r.Items)
			}
		}
		return 0
	}
	if recommendedIn("sam") != 1 {
		t.Fatalf("sam's home: %+v", home.Rows)
	}
	rec := home.RecommendedBy[itoa(movie)]
	if len(rec.From) != 1 || rec.From[0] != "dana" || len(rec.Notes) != 1 || rec.Notes[0] != "dana: You'll love the ending" {
		t.Fatalf("recommended by: %+v", home.RecommendedBy)
	}
	if recommendedIn("shy") != 0 || recommendedIn("kid") != 0 {
		t.Fatal("recommendation reached someone it shouldn't")
	}
	// Watching it takes it off the row; so does dismissing.
	st.SetWatched(sam.ID, []int64{movie}, true)
	if recommendedIn("sam") != 0 {
		t.Fatal("still recommended after watching")
	}
	st.SetWatched(sam.ID, []int64{movie}, false)
	if c := call("sam", "DELETE", "/api/recommendations/"+itoa(movie), "", nil); c != http.StatusNoContent || recommendedIn("sam") != 0 {
		t.Fatalf("dismiss: %d", c)
	}
	// Recommending again brings it back.
	call("dana", "POST", item+"/recommend", `{"userIds":[`+itoa(sam.ID)+`]}`, nil)
	if recommendedIn("sam") != 1 {
		t.Fatal("re-recommending didn't bring it back")
	}
	if c := call("dana", "POST", item+"/recommend", `{"userIds":[]}`, nil); c != http.StatusBadRequest {
		t.Fatalf("no recipients: %d", c)
	}
}
