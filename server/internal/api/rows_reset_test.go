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

// Reset customization puts a library's rows back to the defaults, and a
// collection's page tells admins where it shows.
func TestResetLibraryRowsAndCollectionVisibility(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	col, _ := st.CreateCollection(lib.ID, "Halloween", store.SmartRules{GenreRule: "Horror"})
	h := (&Server{Store: st}).Router(context.Background())
	call := func(method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	if code := call("PUT", "/api/admin/collections/"+itoa(col.ID)+"/visibility", `{"library":true,"home":true,"friends":false}`, nil); code != http.StatusOK {
		t.Fatalf("visibility: %d", code)
	}
	var page struct{ Collection store.Collection }
	if call("GET", "/api/collections/"+itoa(col.ID), "", &page); page.Collection.Visible == nil || !page.Collection.Visible.Home {
		t.Fatalf("collection page visibility: %+v", page.Collection.Visible)
	}
	var rows []store.LibraryRow
	if code := call("DELETE", "/api/admin/libraries/"+itoa(lib.ID)+"/rows", "", &rows); code != http.StatusOK {
		t.Fatalf("reset: %d", code)
	}
	if len(rows) == 0 || rows[0].Key != "continue" {
		t.Fatalf("reset order: %+v", rows)
	}
	for _, r := range rows {
		if r.CollectionID == col.ID && (r.Library || r.Home || r.Friends) {
			t.Fatalf("collection still visible after reset: %+v", r)
		}
	}
}
