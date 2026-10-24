package trakt

import (
	"encoding/json"
	"testing"

	"bingebandit/internal/store"
)

func TestHistoryBody(t *testing.T) {
	b := historyBody([]store.WatchedPlay{
		{Kind: "movie", WatchedAt: 1700000000, Title: "Alpha", Year: 2001, TMDBID: 11},
		{Kind: "movie", WatchedAt: 1700000100, Title: "No Ids", Year: 1999},
		{Kind: "episode", WatchedAt: 1700000200, Title: "Show", TMDBID: 22, Season: 1, Episode: 1},
		{Kind: "episode", WatchedAt: 1700000300, Title: "Show", TMDBID: 22, Season: 1, Episode: 2},
		{Kind: "episode", WatchedAt: 1700000400, Title: "Show", TMDBID: 22, Season: 2, Episode: 1},
		{Kind: "episode", WatchedAt: 1700000500, Title: "Other", Year: 2010, Season: 1, Episode: 5},
	})
	if len(b.Movies) != 2 || b.Movies[0].IDs.TMDB != 11 || b.Movies[0].Title != "" || b.Movies[1].Title != "No Ids" || b.Movies[1].Year != 1999 {
		t.Errorf("movies: %+v", b.Movies)
	}
	if b.Movies[0].WatchedAt != "2023-11-14T22:13:20Z" {
		t.Errorf("watched_at = %q", b.Movies[0].WatchedAt)
	}
	if len(b.Shows) != 2 {
		t.Fatalf("shows: %+v", b.Shows)
	}
	sh := b.Shows[0]
	if sh.IDs.TMDB != 22 || len(sh.Seasons) != 2 || len(sh.Seasons[0].Episodes) != 2 || sh.Seasons[1].Number != 2 {
		t.Errorf("grouping: %+v", sh)
	}
	if b.Shows[1].Title != "Other" || b.Shows[1].Year != 2010 || b.Shows[1].IDs.TMDB != 0 {
		t.Errorf("title fallback: %+v", b.Shows[1])
	}
	// tmdb 0 must not be sent as an id.
	raw, _ := json.Marshal(b.Shows[1].IDs)
	if string(raw) != "{}" {
		t.Errorf("ids = %s", raw)
	}
}
