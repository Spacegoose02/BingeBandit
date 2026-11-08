package store

import (
	"database/sql"
	"strconv"
	"strings"
)

// Thumbs on titles, Netflix style: -1 not for me, 1 I like this, 2 love
// this. A thumbs down keeps the title out of the viewer's recommendation
// rows, "More like this" and the Home hero; thumbs up feed "Because you
// liked" rows. 0 clears a rating.

// SetTitleRating saves (or with 0, clears) uid's thumbs for a movie or show.
func (s *Store) SetTitleRating(uid, itemID int64, rating int) error {
	if rating == 0 {
		_, err := s.db.Exec(`DELETE FROM title_ratings WHERE user_id = ? AND item_id = ?`, uid, itemID)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO title_ratings (user_id, item_id, rating, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET rating = excluded.rating, updated_at = excluded.updated_at`,
		uid, itemID, rating, now())
	return err
}

// TitleRating is uid's thumbs for an item (0: none).
func (s *Store) TitleRating(uid, itemID int64) int {
	var r int
	s.db.QueryRow(`SELECT rating FROM title_ratings WHERE user_id = ? AND item_id = ?`, uid, itemID).Scan(&r)
	return r
}

// Disliked is the set of titles uid gave a thumbs down.
func (s *Store) Disliked(uid int64) map[int64]bool {
	out := map[int64]bool{}
	rows, err := s.db.Query(`SELECT item_id FROM title_ratings WHERE user_id = ? AND rating < 0`, uid)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// RecentlyLiked lists the movies and shows uid most recently gave a thumbs
// up, newest first.
func (s *Store) RecentlyLiked(uid int64, n int) []FinishedTitle {
	rows, err := s.db.Query(`SELECT i.kind, i.tmdb_id, i.title FROM title_ratings r JOIN items i ON i.id = r.item_id
		WHERE r.user_id = ? AND r.rating > 0 AND i.kind IN ('movie', 'show') AND i.tmdb_id > 0
		ORDER BY r.updated_at DESC LIMIT ?`, uid, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []FinishedTitle
	for rows.Next() {
		var f FinishedTitle
		if rows.Scan(&f.Kind, &f.TMDBID, &f.Title) == nil {
			out = append(out, f)
		}
	}
	return out
}

// SimilarByGenre lists movies or shows (of its kind) that share the most
// genres with it, best rated first, for "More like this" when TMDB can't
// be asked or gives too few that the server has.
func (s *Store) SimilarByGenre(uid int64, it *Item, limit int) ([]*Item, error) {
	if len(it.Genres) == 0 || (it.Kind != "movie" && it.Kind != "show") {
		return []*Item{}, nil
	}
	ph := make([]string, len(it.Genres))
	args := []any{uidArg(uid), sql.Named("kind", it.Kind), sql.Named("id", it.ID), sql.Named("limit", limit)}
	for i, g := range it.Genres {
		name := "g" + strconv.Itoa(i)
		ph[i] = "@" + name
		args = append(args, sql.Named(name, g))
	}
	return s.queryItems(listSelect+` JOIN (SELECT i2.id AS sid, COUNT(*) AS shared FROM items i2, json_each(i2.genres) g
			WHERE i2.kind = @kind AND i2.id != @id AND g.value IN (`+strings.Join(ph, ",")+`) GROUP BY i2.id) m ON m.sid = i.id
		WHERE `+visible+` AND NOT EXISTS (SELECT 1 FROM title_ratings tr WHERE tr.user_id = @uid AND tr.item_id = i.id AND tr.rating < 0)
		ORDER BY m.shared DESC, i.rating DESC, i.id LIMIT @limit`, args...)
}
