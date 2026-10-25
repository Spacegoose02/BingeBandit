package importer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// memImages stands in for the artwork folder.
type memImages struct{ saved map[string][]byte }

func (m *memImages) Save(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	ref := fmt.Sprintf("local:/%032x.png", len(m.saved)+1)
	m.saved[ref] = b
	return ref, nil
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPlexArtworkImport(t *testing.T) {
	pic := pngBytes(t)
	var imageHits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.URL.Path, "/thumb/") || strings.Contains(r.URL.Path, "/art/") {
			imageHits = append(imageHits, r.URL.Path)
			w.Header().Set("Content-Type", "image/png")
			w.Write(pic)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path + "?" + r.URL.Query().Get("type") {
		case "/library/sections?":
			io.WriteString(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"},{"key":"2","title":"TV","type":"show"}]}}`)
		case "/library/sections/1/all?1":
			io.WriteString(w, `{"MediaContainer":{"Metadata":[
				{"ratingKey":"10","type":"movie","title":"Alpha","year":2020,"thumb":"/library/metadata/10/thumb/1","art":"/library/metadata/10/art/1",
				 "Media":[{"Part":[{"file":"/m/Alpha (2020)/alpha.mkv"}]}]},
				{"ratingKey":"11","type":"movie","title":"Not Here","year":1990,"thumb":"/library/metadata/11/thumb/1"}]}}`)
		case "/library/sections/2/all?2":
			io.WriteString(w, `{"MediaContainer":{"Metadata":[
				{"ratingKey":"20","type":"show","title":"Beta","year":2015,"Guid":[{"id":"tmdb://77"}],"thumb":"/library/metadata/20/thumb/1","art":"/library/metadata/20/art/1"}]}}`)
		case "/library/sections/2/all?3":
			io.WriteString(w, `{"MediaContainer":{"Metadata":[
				{"ratingKey":"21","type":"season","index":1,"parentRatingKey":"20","parentTitle":"Beta","thumb":"/library/metadata/21/thumb/1","art":"/library/metadata/20/art/1"},
				{"ratingKey":"22","type":"season","index":2,"parentRatingKey":"20","parentTitle":"Beta","thumb":"/library/metadata/20/thumb/1","art":"/library/metadata/20/art/1"}]}}`)
		case "/library/sections/1/collections?":
			io.WriteString(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"90","type":"collection","title":"Faves","thumb":"/library/collections/90/thumb/1"}]}}`)
		case "/library/collections/90/children?":
			io.WriteString(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"10","type":"movie","title":"Alpha","year":2020}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	tv := &store.Library{Name: "TV", Kind: "shows", Paths: []string{}}
	st.CreateLibrary(movies)
	st.CreateLibrary(tv)
	alpha, _ := st.UpsertItem(&store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Alpha", Year: 2020, MatchKey: "movie:alpha"})
	beta, _ := st.UpsertItem(&store.Item{LibraryID: tv.ID, Kind: "show", Title: "Beta", Year: 2015, TMDBID: 77, MatchKey: "show:beta"})
	one, two := 1, 2
	s1, _ := st.UpsertItem(&store.Item{LibraryID: tv.ID, Kind: "season", Title: "Season 1", ParentID: beta, ShowID: beta, SeasonNumber: &one, MatchKey: "season:beta:1"})
	s2, _ := st.UpsertItem(&store.Item{LibraryID: tv.ID, Kind: "season", Title: "Season 2", ParentID: beta, ShowID: beta, SeasonNumber: &two, MatchKey: "season:beta:2"})

	imgs := &memImages{saved: map[string][]byte{}}
	r := &Runner{st: st, Images: imgs, missing: map[string]int{}}
	if err := r.run(context.Background(), NewPlex(srv.URL, "tok"), Options{Artwork: true}); err != nil {
		t.Fatal(err)
	}
	// Alpha poster + background, Beta poster + background, season 1 poster.
	// Season 2 only has its show's images, and "Not Here" isn't in the library.
	if r.status.Artwork != 5 || r.status.ArtFailed != 0 || len(imageHits) != 5 {
		t.Fatalf("artwork %d, failed %d, downloads %v", r.status.Artwork, r.status.ArtFailed, imageHits)
	}
	for _, c := range []struct {
		id     int64
		kind   string
		wantOK bool
	}{{alpha, "poster", true}, {alpha, "backdrop", true}, {beta, "poster", true}, {s1, "poster", true}, {s1, "backdrop", false}, {s2, "poster", false}} {
		ref, err := st.RawArtwork(c.id, c.kind)
		if err != nil {
			t.Fatal(err)
		}
		if got := imgs.saved[ref] != nil; got != c.wantOK {
			t.Errorf("item %d %s = %q, want imported %v", c.id, c.kind, ref, c.wantOK)
		}
	}
	it, err := st.Item(0, alpha)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(it.Locked, ","), "poster") {
		t.Errorf("imported poster isn't locked against metadata refreshes: %v", it.Locked)
	}

	// Collections bring their poster along (collections are imported first).
	imageHits = nil
	colRef := fmt.Sprintf("local:/%032x.png", len(imgs.saved)+1)
	r = &Runner{st: st, Images: imgs, missing: map[string]int{}}
	if err := r.run(context.Background(), NewPlex(srv.URL, "tok"), Options{Collections: true, Artwork: true}); err != nil {
		t.Fatal(err)
	}
	cols, err := st.LibraryCollections(movies.ID)
	if err != nil || len(cols) != 1 {
		t.Fatalf("collections: %v %v", cols, err)
	}
	if cols[0].Poster != store.ImageURL(colRef) {
		t.Errorf("collection poster = %q", cols[0].Poster)
	}

	// A preview downloads nothing.
	imageHits = nil
	r = &Runner{st: st, Images: imgs, missing: map[string]int{}}
	if err := r.run(context.Background(), NewPlex(srv.URL, "tok"), Options{Artwork: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if r.status.Artwork != 5 || len(imageHits) != 0 { // the collection isn't counted without Collections
		t.Errorf("dry run: artwork %d, downloads %v", r.status.Artwork, imageHits)
	}
}
