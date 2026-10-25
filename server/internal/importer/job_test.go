package importer

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

// fakeSource has one collection with a poster only it can serve, and a label.
type fakeSource struct{ fetched []string }

func (f *fakeSource) Kind() string                                { return "fake" }
func (f *fakeSource) Users(context.Context) ([]SourceUser, error) { return nil, nil }
func (f *fakeSource) Records(context.Context, bool, func(Record) error, func(string, int, int)) error {
	return nil
}

func (f *fakeSource) Collections(ctx context.Context, emit func(Collection) error) error {
	alien := Record{Kind: "movie", Title: "Alien", Year: 1979, TMDB: 348}
	if err := emit(Collection{Name: "Scary", Items: []Record{alien}, Poster: "fake:/scary", Label: "Spooky", ItemLabel: "Cinemask"}); err != nil {
		return err
	}
	return emit(Collection{Name: "Space", Items: []Record{alien}, Poster: "tmdb:/space.jpg"})
}

func (f *fakeSource) Image(_ context.Context, ref string) (io.ReadCloser, error) {
	f.fetched = append(f.fetched, ref)
	return io.NopCloser(strings.NewReader("png")), nil
}

func (f *fakeSource) Labels(_ context.Context, emit func(LabelSet) error) error {
	return emit(LabelSet{Name: "Spooky", Items: []Record{{Kind: "movie", Title: "Alien", Year: 1979, TMDB: 348}}})
}

type fakeArt struct{}

func (fakeArt) Save(r io.Reader) (string, error) {
	b, _ := io.ReadAll(r)
	return "local:/" + string(b) + ".png", nil
}

func TestImportCollectionPostersAndLabels(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Alien", Year: 1979, TMDBID: 348, MatchKey: "movie:alien"})

	src := &fakeSource{}
	r := NewRunner(st, fakeArt{})
	opt := Options{Collections: true, Labels: true}
	if err := r.run(context.Background(), src, opt); err != nil {
		t.Fatal(err)
	}
	if s := r.Status(); s.Collections != 2 || s.Labels != 1 {
		t.Errorf("status %+v", s)
	}
	cols, _ := st.LibraryCollections(lib.ID)
	byName := map[string]*store.Collection{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if c := byName["Scary"]; c == nil || c.Poster != store.ImageURL("local:/png.png") || c.LabelRule != "Spooky" {
		t.Errorf("Scary: %+v", c)
	}
	if c := byName["Space"]; c == nil || c.Poster != store.ImageURL("tmdb:/space.jpg") {
		t.Errorf("Space: %+v", c)
	}
	it, _ := st.Item(0, id)
	if strings.Join(it.Labels, ",") != "Cinemask,Spooky" {
		t.Errorf("labels %v", it.Labels)
	}

	// A second run doesn't fetch posters the collections already have.
	if err := r.run(context.Background(), src, opt); err != nil {
		t.Fatal(err)
	}
	if len(src.fetched) != 1 {
		t.Errorf("fetched %v", src.fetched)
	}
}
