package importer

import (
	"reflect"
	"testing"
)

const kometaMovies = `
templates:
  ignored: {}
collections:
  Trending:
    trakt_trending: 40
    tmdb_trending_daily: 40
    tmdb_trending_weekly: 40
    sort_title: +_Trending
    sync_mode: sync
    summary: Movies Trending across the internet
    radarr_add: false
    url_background: https://example.com/bg.png
    url_poster: https://example.com/poster.jpg
  Top Ten Pirated:
    mdblist_list:
      url: https://mdblist.com/lists/someone/top-ten
      sort_by: added.desc
    collection_order: custom
    sort_title: +_1APirated Weekly
  Family Friendly:
    yamtrack_list: https://track.example.com/list/familyfriendly
    smart_label: release.asc
    visible_home: true
    visible_shared: false
  Halloween:
    yamtrack_list: https://track.example.com/list/halloween
    smart_label: audience_rating.desc
    file_poster: posters/halloween.png
  Top Rated:
    mdblist_list:
      url: https://mdblist.com/lists/someone/top-rated
      sort_by: audience_rating.desc
    collection_order: custom
  Christmas:
    imdb_list:
      url: https://www.imdb.com/list/ls000096828/
    imdb_chart: top_movies
    smart_label: originally_available.desc
  By Hand:
    plex_search:
      all:
        genre: Horror
`

func TestParseKometa(t *testing.T) {
	cols, err := ParseKometa([]byte(kometaMovies), "movies")
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 7 {
		t.Fatalf("%d collections", len(cols))
	}
	tr := cols[0]
	if tr.Name != "Trending" || tr.SortTitle != "+_Trending" || tr.Summary != "Movies Trending across the internet" ||
		tr.Poster != "https://example.com/poster.jpg" || tr.Backdrop != "https://example.com/bg.png" || tr.Sort != "" || len(tr.Skipped) != 0 {
		t.Fatalf("Trending: %+v", tr)
	}
	if want := []string{"https://trakt.tv/movies/trending?limit=40", "https://www.themoviedb.org/trending/movie/day?limit=40", "https://www.themoviedb.org/trending/movie/week?limit=40"}; !reflect.DeepEqual(tr.Lists, want) {
		t.Fatalf("Trending lists %v", tr.Lists)
	}
	sorts := map[string]string{"Top Ten Pirated": "list", "Family Friendly": "", "Halloween": "rating", "Top Rated": "rating", "Christmas": "release_desc"}
	for _, c := range cols {
		if want, ok := sorts[c.Name]; ok && c.Sort != want {
			t.Errorf("%s sorts %q, want %q", c.Name, c.Sort, want)
		}
	}
	if ff := cols[2]; ff.VisibleHome == nil || !*ff.VisibleHome || ff.VisibleShared == nil || *ff.VisibleShared || ff.VisibleLibrary != nil {
		t.Errorf("Family Friendly visibility: %+v", ff)
	}
	if h := cols[3]; len(h.Skipped) != 1 {
		t.Errorf("Halloween's file_poster should be reported: %v", h.Skipped)
	}
	if c := cols[5]; !reflect.DeepEqual(c.Lists, []string{"https://www.imdb.com/list/ls000096828/", "https://www.imdb.com/chart/top/"}) {
		t.Errorf("Christmas lists %v", c.Lists)
	}
	if c := cols[6]; len(c.Lists) != 0 || len(c.Skipped) != 2 {
		t.Errorf("plex_search should be reported, with no list: %+v", c)
	}

	shows, _ := ParseKometa([]byte("collections:\n  Hot:\n    trakt_trending: 10\n    tmdb_popular: 20\n    tmdb_now_playing: 5\n"), "shows")
	if want := []string{"https://trakt.tv/shows/trending?limit=10", "https://www.themoviedb.org/tv?limit=20"}; !reflect.DeepEqual(shows[0].Lists, want) || len(shows[0].Skipped) != 1 {
		t.Errorf("TV builders: %+v", shows[0])
	}
	if _, err := ParseKometa([]byte("libraries: {}"), "movies"); err == nil {
		t.Error("a file with no collections should be refused")
	}
}
