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

func TestImportKometa(t *testing.T) {
	kometaBackground = func(func()) {} // no posters or list syncs here
	defer func() { kometaBackground = func(f func()) { go f() } }()
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	old, _ := st.CreateCollection(lib.ID, "Halloween", store.SmartRules{GenreRule: "Horror"})
	h := (&Server{Store: st}).Router(context.Background())
	post := func(query, body string) (int, []kometaResult) {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/admin/libraries/"+itoa(lib.ID)+"/kometa"+query, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out struct{ Collections []kometaResult }
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out.Collections
	}
	file := `collections:
  Halloween:
    yamtrack_list: https://track.example.com/list/halloween
    sort_title: +++_Halloween
    smart_label: audience_rating.desc
    visible_home: true
    visible_shared: true
  Apple:
    mdblist_list:
      url: https://mdblist.com/lists/someone/apple
    sort_title: ++_APPLE
  Trending:
    trakt_trending: 40
    sort_title: +_Trending
    summary: Movies Trending across the internet
`
	code, plan := post("?dryRun=1&recommended=1", file)
	if code != http.StatusOK || len(plan) != 3 || !plan[0].Exists || plan[1].Exists || !plan[0].Visible.Home || !plan[1].Visible.Library {
		t.Fatalf("dry run: %d %+v", code, plan)
	}
	if cols, _ := st.LibraryCollections(lib.ID); len(cols) != 1 {
		t.Fatal("a dry run changed the library")
	}
	if code, _ = post("?recommended=1", file); code != http.StatusOK {
		t.Fatalf("import: %d", code)
	}
	cols, _ := st.LibraryCollections(lib.ID)
	var names []string
	for _, c := range cols {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "Trending,Apple,Halloween" {
		t.Fatalf("collections in sort-title order: %v", names)
	}
	hw, _ := st.Collection(old.ID)
	if hw.ListURL != "https://track.example.com/list/halloween" || hw.Sort != "rating" || hw.SortTitle != "+++_Halloween" || hw.GenreRule != "Horror" {
		t.Fatalf("Halloween updated in place, keeping its rules: %+v", hw)
	}
	rows, _ := st.LibraryRows(lib)
	var order []string
	for _, r := range rows {
		if r.CollectionID != 0 {
			order = append(order, r.Title)
			if r.Title == "Halloween" && (!r.Home || !r.Friends || !r.Library) {
				t.Errorf("Halloween row: %+v", r)
			}
			if r.Title == "Apple" && (r.Home || !r.Library) {
				t.Errorf("Apple row: %+v", r)
			}
		}
	}
	if strings.Join(order, ",") != "Trending,Apple,Halloween" {
		t.Fatalf("rows in sort-title order: %v", order)
	}
	// Importing again changes nothing new.
	if code, _ = post("", file); code != http.StatusOK {
		t.Fatalf("again: %d", code)
	}
	if cols, _ = st.LibraryCollections(lib.ID); len(cols) != 3 {
		t.Fatalf("re-import made %d collections", len(cols))
	}
}
