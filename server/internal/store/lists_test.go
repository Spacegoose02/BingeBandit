package store

import (
	"testing"
	"time"
)

func TestPlaylistsWatchlistAndHistory(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	a := addMovie(t, s, lib.ID, "Alpha")
	b := addMovie(t, s, lib.ID, "Bravo")
	c := addMovie(t, s, lib.ID, "Charlie")
	u, _ := s.CreateUser("viewer", "x", false)

	// Playlists keep order, skip duplicates, reorder and find the next item.
	p, err := s.CreatePlaylist(u.ID, "Weekend")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := s.AddToPlaylist(p.ID, []int64{a, b, a, c}); n != 3 {
		t.Errorf("added %d, want 3 (duplicate skipped)", n)
	}
	s.ReorderPlaylist(p.ID, []int64{c, a, b})
	items, _ := s.PlaylistItems(u.ID, p.ID)
	if len(items) != 3 || items[0].ID != c || items[1].ID != a || items[2].ID != b {
		t.Errorf("order after reorder is wrong")
	}
	if next, err := s.PlaylistNext(u.ID, p.ID, a); err != nil || next.ID != b {
		t.Errorf("next after Alpha should be Bravo, got %v %v", next, err)
	}
	if _, err := s.PlaylistNext(u.ID, p.ID, b); err != ErrNotFound {
		t.Errorf("nothing follows the last item, got %v", err)
	}
	other, _ := s.CreateUser("other", "x", false)
	if _, err := s.Playlist(other.ID, p.ID); err != ErrNotFound {
		t.Error("playlists must be private to their owner")
	}

	// Watchlist membership shows up on items for that user only.
	s.AddToWatchlist(u.ID, b)
	if it, _ := s.Item(u.ID, b); !it.OnWatchlist {
		t.Error("item should be on the watchlist")
	}
	if it, _ := s.Item(other.ID, b); it.OnWatchlist {
		t.Error("watchlists are per user")
	}

	// History and stats.
	start := time.Now().Add(-time.Hour).Unix()
	id, err := s.StartPlay(&Play{UserID: u.ID, ItemID: a, Title: "Alpha", Kind: "movie", LibraryID: lib.ID, StartedAt: start, StoppedAt: start, Watched: 60, Mode: "direct", Client: "Web"})
	if err != nil {
		t.Fatal(err)
	}
	s.UpdatePlay(id, start+3600, 3600, true, "direct")
	h, _ := s.History(u.ID, 10, 0)
	if len(h) != 1 || h[0].Watched != 3600 || !h[0].Completed {
		t.Errorf("history = %+v", h)
	}
	st, err := s.UserStats(u.ID)
	if err != nil || st.Plays != 1 || st.Movies != 1 || st.Hours != 1 {
		t.Errorf("user stats = %+v, %v", st, err)
	}
	ss, err := s.ServerStats(7)
	if err != nil || len(ss.Daily) != 7 || ss.TotalPlays != 1 || len(ss.TopMovies) != 1 || ss.TopMovies[0].Key != "Alpha" {
		t.Errorf("server stats = %+v, %v", ss, err)
	}
}
