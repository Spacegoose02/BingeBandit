package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/plugins"
	"bingebandit/internal/store"
)

func TestPluginCatalogAPI(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	ann, _ := st.CreateUser("ann", "x", false)
	st.CreateSession("admin-token", admin.ID, "test", "127.0.0.1")
	st.CreateSession("ann-token", ann.ID, "test", "127.0.0.1")

	cats := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official.json":
			http.NotFound(w, r) // no release has one yet
		case "/extra.json":
			json.NewEncoder(w).Encode(plugins.Catalog{Plugins: []plugins.CatalogEntry{{ID: "extra", Name: "Extra", Version: "1.0.0"}}})
		}
	}))
	defer cats.Close()
	s := &Server{Store: st}
	s.Plugins = plugins.NewHost(st, nil, filepath.Join(dir, "plugin-data"), "")
	s.Plugins.Official = cats.URL + "/official.json"
	s.Plugins.InstallDir = filepath.Join(dir, "plugins")
	h := s.Router(context.Background())
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

	if c := call("ann-token", "GET", "/api/admin/plugins/catalog", "", nil); c != http.StatusForbidden {
		t.Fatalf("non-admin: %d", c)
	}
	if c := call("admin-token", "PUT", "/api/admin/settings", `{"plugin_catalogs":"ftp://nope"}`, nil); c != http.StatusBadRequest {
		t.Fatalf("bad catalogue address accepted: %d", c)
	}
	if c := call("admin-token", "PUT", "/api/admin/settings", `{"plugin_catalogs":"`+cats.URL+`/extra.json\n"}`, nil); c != http.StatusOK {
		t.Fatalf("save catalogues: %d", c)
	}
	var got struct {
		Plugins  []plugins.CatalogPlugin `json:"plugins"`
		Errors   []plugins.CatalogError  `json:"errors"`
		Catalogs []string                `json:"catalogs"`
	}
	call("admin-token", "GET", "/api/admin/plugins/catalog", "", &got)
	if len(got.Catalogs) != 2 || len(got.Plugins) != 1 || got.Plugins[0].ID != "extra" || len(got.Errors) != 1 ||
		!strings.Contains(got.Errors[0].Error, "next release") {
		t.Fatalf("catalog: %+v", got)
	}
	// Only from listed catalogues.
	if c := call("admin-token", "POST", "/api/admin/plugins/catalog/install", `{"catalog":"http://elsewhere/x.json","id":"extra"}`, nil); c != http.StatusBadRequest {
		t.Fatalf("unlisted catalogue: %d", c)
	}
	// Listed, but no download for this platform.
	if c := call("admin-token", "POST", "/api/admin/plugins/catalog/install", `{"catalog":"`+cats.URL+`/extra.json","id":"extra"}`, nil); c != http.StatusBadGateway {
		t.Fatalf("no download: %d", c)
	}
}
