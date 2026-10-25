package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ShowAlias returns the show a folder's match key was merged into, with that
// show's own match key, or ok false when the key isn't an alias.
func (s *Store) ShowAlias(libraryID int64, matchKey string) (showID int64, showKey string, ok bool) {
	err := s.db.QueryRow(`SELECT a.show_id, i.match_key FROM show_aliases a JOIN items i ON i.id = a.show_id
		WHERE a.library_id = ? AND a.match_key = ?`, libraryID, matchKey).Scan(&showID, &showKey)
	return showID, showKey, err == nil
}

// MergedShow is one show folded into another by MergeDuplicateShows.
type MergedShow struct {
	Title  string
	KeptID int64
}

// Tables whose rows follow a merged show or episode to the one kept. A row
// the kept item already has (the same user's watch state, say) stays with
// the duplicate and goes when it is deleted.
var mergeTables = []string{"watch_state", "watchlist", "collection_items", "playlist_items", "markers",
	"plays", "playout_items", "external_ratings", "recommendations"}

// MergeDuplicateShows folds shows of a library that matched the same TMDB or
// TVDB show into one, as Plex does: seasons and episodes come together (two
// copies of an episode become two versions of it) along with watch state,
// watchlists, collections and playlists. The show with the most episodes is
// kept.
func (s *Store) MergeDuplicateShows(libraryID int64) ([]MergedShow, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	type show struct {
		id         int64
		key, title string
	}
	var merged []MergedShow
	for _, col := range []string{"tmdb_id", "tvdb_id"} {
		rows, err := tx.Query(fmt.Sprintf(`SELECT s.id, s.match_key, s.title, s.%[1]s FROM items s
			WHERE s.library_id = ? AND s.kind = 'show' AND s.%[1]s > 0 AND s.%[1]s IN (
				SELECT %[1]s FROM items WHERE library_id = ? AND kind = 'show' AND %[1]s > 0 GROUP BY %[1]s HAVING COUNT(*) > 1)
			ORDER BY s.%[1]s, (SELECT COUNT(*) FROM items e WHERE e.show_id = s.id AND e.kind = 'episode') DESC, s.id`, col),
			libraryID, libraryID)
		if err != nil {
			return nil, err
		}
		var groups [][]show
		last := int64(-1)
		for rows.Next() {
			var sh show
			var ext int64
			if err := rows.Scan(&sh.id, &sh.key, &sh.title, &ext); err != nil {
				rows.Close()
				return nil, err
			}
			if ext != last {
				groups = append(groups, nil)
				last = ext
			}
			groups[len(groups)-1] = append(groups[len(groups)-1], sh)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, g := range groups {
			keep := g[0]
			for _, dup := range g[1:] {
				if err := mergeShow(tx, libraryID, keep.id, keep.key, dup.id, dup.key); err != nil {
					return nil, fmt.Errorf("merge %q: %w", dup.title, err)
				}
				merged = append(merged, MergedShow{Title: dup.title, KeptID: keep.id})
			}
		}
	}
	return merged, tx.Commit()
}

func mergeShow(tx *sql.Tx, libraryID, keepID int64, keepKey string, dupID int64, dupKey string) error {
	// Later scans of the duplicate's folder (and of folders merged into it
	// before) add to the kept show.
	if _, err := tx.Exec(`UPDATE show_aliases SET show_id = ? WHERE show_id = ?`, keepID, dupID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO show_aliases (library_id, match_key, show_id) VALUES (?, ?, ?)
		ON CONFLICT (library_id, match_key) DO UPDATE SET show_id = excluded.show_id`, libraryID, dupKey, keepID); err != nil {
		return err
	}

	type ep struct {
		id, seasonID     int64
		season, episode  int
		title, seasonTtl string
	}
	rows, err := tx.Query(`SELECT e.id, e.parent_id, COALESCE(e.season_number, 0), COALESCE(e.episode_number, 0), e.title, COALESCE(p.title, '')
		FROM items e LEFT JOIN items p ON p.id = e.parent_id WHERE e.show_id = ? AND e.kind = 'episode'`, dupID)
	if err != nil {
		return err
	}
	var eps []ep
	for rows.Next() {
		var e ep
		var parent sql.NullInt64
		if err := rows.Scan(&e.id, &parent, &e.season, &e.episode, &e.title, &e.seasonTtl); err != nil {
			rows.Close()
			return err
		}
		e.seasonID = parent.Int64
		eps = append(eps, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	t := now()
	for _, e := range eps {
		// The same INSERT … ON CONFLICT as a scan would make, so a season or
		// episode the kept show already has is reused.
		var seasonID, epID int64
		seasonKey := fmt.Sprintf("%s:s%d", keepKey, e.season)
		if err := tx.QueryRow(`INSERT INTO items (library_id, kind, parent_id, show_id, match_key, title, sort_title, season_number, added_at, updated_at)
			VALUES (?, 'season', ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (library_id, match_key) DO UPDATE SET parent_id = excluded.parent_id RETURNING id`,
			libraryID, keepID, keepID, seasonKey, e.seasonTtl, SortTitle(e.seasonTtl), e.season, t, t).Scan(&seasonID); err != nil {
			return err
		}
		if err := tx.QueryRow(`INSERT INTO items (library_id, kind, parent_id, show_id, match_key, title, sort_title, season_number, episode_number, added_at, updated_at)
			VALUES (?, 'episode', ?, ?, ?, ?, ?, ?, ?, (SELECT added_at FROM items WHERE id = ?), ?)
			ON CONFLICT (library_id, match_key) DO UPDATE SET parent_id = excluded.parent_id RETURNING id`,
			libraryID, seasonID, keepID, fmt.Sprintf("%s:e%d", seasonKey, e.episode), e.title, SortTitle(e.title), e.season, e.episode, e.id, t).Scan(&epID); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE media_files SET item_id = ? WHERE item_id = ?`, epID, e.id); err != nil {
			return err
		}
		if err := moveRows(tx, e.id, epID); err != nil {
			return err
		}
	}
	if err := moveRows(tx, dupID, keepID); err != nil {
		return err
	}
	// Its seasons and episodes, now without files, go with it.
	_, err = tx.Exec(`DELETE FROM items WHERE id = ?`, dupID)
	return err
}

func moveRows(tx *sql.Tx, from, to int64) error {
	for _, t := range mergeTables {
		if _, err := tx.Exec(`UPDATE OR IGNORE `+t+` SET item_id = ? WHERE item_id = ?`, to, from); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return nil
}
