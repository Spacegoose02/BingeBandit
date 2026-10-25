package importer

import (
	"testing"

	"bingebandit/internal/store"
)

func TestMatcher(t *testing.T) {
	items := []*store.IndexItem{
		{ID: 1, Kind: "movie", Title: "The Matrix", Year: 1999, TMDBID: 603, Files: []string{"the.matrix.1999.mkv"}},
		{ID: 2, Kind: "movie", Title: "Dune", Year: 1984},
		{ID: 3, Kind: "movie", Title: "Dune", Year: 2021},
		{ID: 4, Kind: "movie", Title: "Amélie & Friends", Year: 2001},
		{ID: 10, Kind: "show", Title: "The Office", Year: 2005, TMDBID: 2316},
		{ID: 11, Kind: "episode", ShowID: 10, Season: 2, Episode: 3, Files: []string{"office.s02e03.mkv"}},
		{ID: 12, Kind: "episode", ShowID: 10, Season: 0, Episode: 1},
		{ID: 20, Kind: "show", Title: "Doctor Who", Year: 1963},
		{ID: 21, Kind: "show", Title: "Doctor Who", Year: 2005},
		{ID: 22, Kind: "episode", ShowID: 21, Season: 1, Episode: 1},
		{ID: 30, Kind: "season", ShowID: 10, Season: 2},
	}
	m := NewMatcher(items)
	for _, c := range []struct {
		name string
		r    Record
		want int64
	}{
		{"file wins", Record{Kind: "movie", Title: "Something Else", File: "The.Matrix.1999.MKV"}, 1},
		{"tmdb", Record{Kind: "movie", Title: "Matrix, The", TMDB: 603}, 1},
		{"title + year picks the remake", Record{Kind: "movie", Title: "Dune", Year: 2021}, 3},
		{"year off by one", Record{Kind: "movie", Title: "Dune", Year: 1985}, 2},
		{"ambiguous title, no year", Record{Kind: "movie", Title: "Dune"}, 0},
		{"accents and &", Record{Kind: "movie", Title: "Amelie and Friends", Year: 2001}, 4},
		{"episode by show tmdb", Record{Kind: "episode", ShowTitle: "Whatever", TMDB: 2316, Season: 2, Episode: 3}, 11},
		{"episode by show title with country", Record{Kind: "episode", ShowTitle: "The Office (US)", Season: 2, Episode: 3}, 11},
		{"special", Record{Kind: "episode", ShowTitle: "The Office", Season: 0, Episode: 1}, 12},
		{"episode by file", Record{Kind: "episode", ShowTitle: "Nope", File: "office.S02E03.mkv"}, 11},
		{"show year decides", Record{Kind: "episode", ShowTitle: "Doctor Who", ShowYear: 2005, Season: 1, Episode: 1}, 22},
		{"show ambiguous without year", Record{Kind: "episode", ShowTitle: "Doctor Who", Season: 1, Episode: 1}, 0},
		{"missing episode", Record{Kind: "episode", ShowTitle: "The Office", Season: 9, Episode: 9}, 0},
		{"collection show", Record{Kind: "show", Title: "the office"}, 10},
		{"season by show tmdb", Record{Kind: "season", ShowTitle: "Whatever", TMDB: 2316, Season: 2}, 30},
		{"season missing here", Record{Kind: "season", ShowTitle: "The Office", Season: 5}, 0},
	} {
		var got int64
		if it := m.Match(&c.r); it != nil {
			got = it.ID
		}
		if got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestNormTitle(t *testing.T) {
	for in, want := range map[string]string{"The Office": "office", "Tom & Jerry": "tom and jerry", "  M*A*S*H ": "m a s h", "A": "a"} {
		if got := normTitle(in); got != want {
			t.Errorf("normTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
