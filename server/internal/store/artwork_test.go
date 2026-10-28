package store

import (
	"slices"
	"testing"
)

func TestTMDBArtworkListsEachImageOnce(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b"} {
		id, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "movie", Title: key, MatchKey: key})
		if err != nil {
			t.Fatal(err)
		}
		// Both share a poster; local artwork isn't TMDB's to download.
		if _, err := s.db.Exec(`UPDATE items SET poster = 'tmdb:/p.jpg', backdrop = ?, logo = 'local:/l.png' WHERE id = ?`, "tmdb:/"+key+".jpg", id); err != nil {
			t.Fatal(err)
		}
	}
	posters, backdrops, logos, err := s.TMDBArtwork()
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(backdrops)
	if !slices.Equal(posters, []string{"p.jpg"}) || !slices.Equal(backdrops, []string{"a.jpg", "b.jpg"}) || len(logos) != 0 {
		t.Fatalf("posters %v, backdrops %v, logos %v", posters, backdrops, logos)
	}
}
