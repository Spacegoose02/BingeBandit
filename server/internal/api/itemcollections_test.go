package api

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// The TV app's "Add to collection" reads and replaces a title's collections.
func TestItemCollections(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "TV", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", MatchKey: "movie:heat"})
	st.CreateCollection(lib.ID, "Crime", store.SmartRules{})
	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	call := func(method, body string) string {
		req := httptest.NewRequest(method, "/api/admin/items/"+itoa(id)+"/collections", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code >= 300 {
			t.Fatalf("%s: %d %s", method, rec.Code, rec.Body)
		}
		return strings.TrimSpace(rec.Body.String())
	}
	if got := call("GET", ""); got != `{"collections":[],"library":["Crime"]}` {
		t.Fatalf("before: %s", got)
	}
	call("PUT", `{"collections":["Crime","Heists"]}`)
	if got := call("GET", ""); got != `{"collections":["Crime","Heists"],"library":["Crime","Heists"]}` {
		t.Fatalf("after: %s", got)
	}
}
