package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

func TestLocalTrailers(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sam, _ := st.CreateUser("sam", "x", false)
	lee, _ := st.CreateUser("lee", "x", false)
	st.CreateSession("sam-token", sam.ID, "Web", "")
	st.CreateSession("lee-token", lee.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{dir}}
	st.CreateLibrary(lib)
	st.SetUserLibraries(sam.ID, true, nil)
	st.SetUserLibraries(lee.ID, false, nil)

	movieDir := filepath.Join(dir, "Heat (1995)")
	os.MkdirAll(movieDir, 0o755)
	os.WriteFile(filepath.Join(movieDir, "Heat (1995).mkv"), []byte("movie"), 0o644)
	os.WriteFile(filepath.Join(movieDir, "Heat (1995)-trailer.mp4"), []byte("trailer bytes"), 0o644)
	heat, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", MatchKey: "movie:heat"})
	if err := st.UpsertFile(&store.MediaFile{ItemID: heat, Path: filepath.Join(movieDir, "Heat (1995).mkv")}, 1); err != nil {
		t.Fatal(err)
	}
	other, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Other", MatchKey: "movie:other"})
	st.UpdateMetadata(other, &store.Item{Title: "Other", SortTitle: "Other"})
	prefs, _ := st.UserPrefs(sam.ID)
	prefs.PreRollTrailers = 1
	st.SetUserPrefs(sam.ID, prefs)

	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	call := func(token, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	var detail struct {
		Trailers []LocalTrailer `json:"trailers"`
	}
	rec := call("sam-token", "/api/items/"+itoa(heat))
	json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Trailers) != 1 || detail.Trailers[0].Name != "Heat (1995)-trailer" {
		t.Fatalf("trailers = %+v (%s)", detail.Trailers, rec.Body)
	}
	if rec := call("sam-token", detail.Trailers[0].URL); rec.Code != http.StatusOK || rec.Body.String() != "trailer bytes" {
		t.Fatalf("trailer file: %d %q", rec.Code, rec.Body)
	}
	if rec := call("lee-token", detail.Trailers[0].URL); rec.Code != http.StatusNotFound {
		t.Fatalf("trailer for someone who can't see the movie: %d", rec.Code)
	}
	if rec := call("sam-token", "/api/items/"+itoa(heat)+"/trailers/1"); rec.Code != http.StatusNotFound {
		t.Fatalf("missing trailer: %d", rec.Code)
	}

	// Before another movie, Heat's own trailer file plays (it has no YouTube trailer).
	var pre struct {
		Trailers []PreRollTrailer `json:"trailers"`
	}
	json.Unmarshal(call("sam-token", "/api/items/"+itoa(other)+"/preroll").Body.Bytes(), &pre)
	if len(pre.Trailers) != 1 || pre.Trailers[0].ItemID != heat || pre.Trailers[0].URL != detail.Trailers[0].URL {
		t.Fatalf("preroll = %+v", pre.Trailers)
	}
}
