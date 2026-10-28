package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

// Cinemask pages through every file and its quality with an admin's key; other users can't.
func TestAdminFiles(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("admin", "x", true)
	st.CreateSession("admin-token", admin.ID, "Cinemask", "")
	ann, _ := st.CreateUser("ann", "x", false)
	st.CreateSession("ann-token", ann.ID, "Web", "")
	lib := &store.Library{Name: "Shows", Kind: "shows", Paths: []string{dir}}
	st.CreateLibrary(lib)
	show, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "show", Title: "Bluey", TMDBID: 82728, MatchKey: "show:bluey"})
	one, two := 1, 2
	ep, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "episode", ShowID: show, ParentID: show, Title: "Hospital", SeasonNumber: &one, EpisodeNumber: &two, MatchKey: "ep:bluey:1:2"})
	if err := st.UpsertFile(&store.MediaFile{ItemID: ep, Path: filepath.Join(dir, "Bluey S01E02.mkv"), Size: 9, VideoCodec: "hevc", Width: 3840, Height: 2160}, 1); err != nil {
		t.Fatal(err)
	}
	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	get := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/admin/files?limit=10", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("ann-token"); rec.Code != 403 {
		t.Fatalf("non-admin: %d", rec.Code)
	}
	rec := get("admin-token")
	var out struct {
		Files []store.FileSummary `json:"files"`
		Total int                 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f := out.Files
	if out.Total != 1 || len(f) != 1 || f[0].ItemID != ep || f[0].ShowID != show || f[0].ShowTMDBID != 82728 || *f[0].Season != 1 || *f[0].Episode != 2 || f[0].Height != 2160 {
		t.Fatalf("files: %+v", out)
	}
}
