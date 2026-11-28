package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// A title's collection shelf includes the same-named collections in the other
// libraries the viewer may see, as the collection page does, so a Christmas
// special's "Boss Baby" row shows the films and shows kept elsewhere.
func TestItemShelfSameNamed(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "TV", "")
	kid, _ := st.CreateUser("kid", "x", false)
	st.CreateSession("kid-token", kid.ID, "TV", "")
	xmas := &store.Library{Name: "Christmas", Kind: "movies", Paths: []string{dir}}
	kids := &store.Library{Name: "Kids Movies", Kind: "movies", Paths: []string{dir}}
	shows := &store.Library{Name: "Kids TV Shows", Kind: "shows", Paths: []string{dir}}
	for _, l := range []*store.Library{xmas, kids, shows} {
		st.CreateLibrary(l)
	}
	st.SetUserLibraries(kid.ID, false, []int64{xmas.ID, kids.ID})
	add := func(lib *store.Library, kind, title string, year int) int64 {
		id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: kind, Title: title, MatchKey: kind + ":" + title, Year: year})
		st.SetItemCollections(id, lib.ID, []string{"The Boss Baby Collection"})
		return id
	}
	bonus := add(xmas, "movie", "Christmas Bonus", 2022)
	add(kids, "movie", "The Boss Baby", 2017)
	add(kids, "movie", "Family Business", 2021)
	add(shows, "show", "Back in Business", 2018)
	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	shelf := func(token string) string {
		req := httptest.NewRequest("GET", "/api/items/"+itoa(bonus), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", token, rec.Code, rec.Body)
		}
		var body struct {
			Collections []struct {
				Collection struct {
					Name string `json:"name"`
				} `json:"collection"`
				Items []struct {
					Title string `json:"title"`
				} `json:"items"`
			} `json:"collections"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		out := []string{}
		for _, c := range body.Collections {
			titles := []string{}
			for _, it := range c.Items {
				titles = append(titles, it.Title)
			}
			out = append(out, c.Collection.Name+": "+strings.Join(titles, ", "))
		}
		return strings.Join(out, " | ")
	}
	if got := shelf("admin-token"); got != "The Boss Baby Collection: The Boss Baby, Back in Business, Family Business, Christmas Bonus" {
		t.Fatalf("admin: %s", got)
	}
	// The kid can't see Kids TV Shows, so the show stays out of their row.
	if got := shelf("kid-token"); got != "The Boss Baby Collection: The Boss Baby, Family Business, Christmas Bonus" {
		t.Fatalf("kid: %s", got)
	}
}
