package library

import "testing"

func TestParseTitleYear(t *testing.T) {
	cases := []struct {
		in    string
		title string
		year  int
		tmdb  int64
	}{
		{"The Matrix (1999)", "The Matrix", 1999, 0},
		{"Blade.Runner.2049.2017.1080p.BluRay.x264", "Blade Runner 2049", 2017, 0},
		{"Blade Runner 2049 (2017)", "Blade Runner 2049", 2017, 0},
		{"1917 (2019)", "1917", 2019, 0},
		{"2012", "2012", 0, 0},
		{"Mr. Nobody (2009) [1080p]", "Mr. Nobody", 2009, 0},
		{"Dune Part Two (2024) {tmdb-693134}", "Dune Part Two", 2024, 693134},
		{"Some_Movie_2010_720p", "Some Movie", 2010, 0},
		{"Arrival", "Arrival", 0, 0},
		{"Inception (2010) [imdbid-tt1375666]", "Inception", 2010, 0},
		{"The Office (US) (2005) {tvdb-73244}", "The Office (US)", 2005, 0},
	}
	for _, c := range cases {
		title, year, tmdb := parseTitleYear(c.in)
		if title != c.title || year != c.year || tmdb != c.tmdb {
			t.Errorf("parseTitleYear(%q) = %q, %d, %d; want %q, %d, %d", c.in, title, year, tmdb, c.title, c.year, c.tmdb)
		}
	}
}

func TestIDHints(t *testing.T) {
	cases := []struct {
		in   string
		tvdb int64
		imdb string
	}{
		{"The Office (US) (2005) {tvdb-73244}", 73244, ""},
		{"Inception (2010) [imdbid-tt1375666]", 0, "tt1375666"},
		{"Show {tvdbid=81189} {imdb-TT0903747}", 81189, "tt0903747"},
		{"Arrival (2016)", 0, ""},
	}
	for _, c := range cases {
		if tvdb, imdb := idHints(c.in); tvdb != c.tvdb || imdb != c.imdb {
			t.Errorf("idHints(%q) = %d, %q; want %d, %q", c.in, tvdb, imdb, c.tvdb, c.imdb)
		}
	}
}

func TestParseEpisode(t *testing.T) {
	cases := []struct {
		in      string
		s, e    int
		ok      bool
		epTitle string
	}{
		{"Breaking Bad - S01E02 - Cat's in the Bag", 1, 2, true, "Cat's in the Bag"},
		{"show.name.s02e10.720p.hdtv", 2, 10, true, ""},
		{"Show 3x07 Title Here", 3, 7, true, "Title Here"},
		{"S01E01E02 Pilot", 1, 1, true, "Pilot"},
		{"The Daily Show - 2026-10-23", 2026, 10230000, true, ""},
		{"Bandit News - 2026-10-23 19.30", 2026, 10231930, true, ""},
		{"Random file", 0, 0, false, ""},
	}
	for _, c := range cases {
		s, e, _, rest, ok := parseEpisode(c.in)
		if s != c.s || e != c.e || ok != c.ok {
			t.Errorf("parseEpisode(%q) = %d, %d, %v; want %d, %d, %v", c.in, s, e, ok, c.s, c.e, c.ok)
		}
		if ok && episodeTitle(rest) != c.epTitle {
			t.Errorf("episodeTitle(%q) = %q; want %q", rest, episodeTitle(rest), c.epTitle)
		}
	}
}

func TestParseSeasonDir(t *testing.T) {
	for in, want := range map[string]int{"Season 1": 1, "season 12": 12, "S03": 3, "Specials": 0, "Series 2": 2} {
		if got, ok := parseSeasonDir(in); !ok || got != want {
			t.Errorf("parseSeasonDir(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	if _, ok := parseSeasonDir("Extras"); ok {
		t.Error("parseSeasonDir(Extras) should fail")
	}
}
