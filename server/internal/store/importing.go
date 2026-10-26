package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IndexItem is the slice of an item the importers match against.
type IndexItem struct {
	ID        int64
	LibraryID int64
	Kind      string
	Title     string
	Year      int
	TMDBID    int64
	IMDBID    string // movies and shows
	TVDBID    int64  // shows
	ShowID    int64
	Season    int
	Episode   int
	Files     []string // lower-case base names
}

// ImportIndex loads every movie, show, season and episode with its file names.
func (s *Store) ImportIndex() ([]*IndexItem, error) {
	rows, err := s.db.Query(`SELECT id, library_id, kind, title, year, tmdb_id, imdb_id, tvdb_id, COALESCE(show_id, 0),
		COALESCE(season_number, 0), COALESCE(episode_number, 0) FROM items`)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*IndexItem{}
	var out []*IndexItem
	for rows.Next() {
		var it IndexItem
		if err := rows.Scan(&it.ID, &it.LibraryID, &it.Kind, &it.Title, &it.Year, &it.TMDBID, &it.IMDBID, &it.TVDBID, &it.ShowID, &it.Season, &it.Episode); err != nil {
			rows.Close()
			return nil, err
		}
		byID[it.ID] = &it
		out = append(out, &it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	frows, err := s.db.Query(`SELECT item_id, path FROM media_files`)
	if err != nil {
		return nil, err
	}
	defer frows.Close()
	for frows.Next() {
		var id int64
		var p string
		if err := frows.Scan(&id, &p); err != nil {
			return nil, err
		}
		if it := byID[id]; it != nil {
			// Paths may come from Windows or Linux; take the last element of either.
			p = p[strings.LastIndexAny(p, `/\`)+1:]
			it.Files = append(it.Files, strings.ToLower(p))
		}
	}
	return out, frows.Err()
}

// ImportPlay adds an imported history row unless one with the same key
// exists, or the same user already has a play of the item that ended within
// 15 minutes (the same viewing seen by both Plex and Tautulli, or one
// BingeBandit recorded itself).
func (s *Store) ImportPlay(p *Play, key string) (bool, error) {
	var item any
	if p.ItemID != 0 {
		item = p.ItemID
		var dup int
		s.db.QueryRow(`SELECT COUNT(*) FROM plays WHERE user_id = ? AND item_id = ? AND ABS(stopped_at - ?) < 900`,
			p.UserID, p.ItemID, p.StoppedAt).Scan(&dup)
		if dup > 0 {
			if traktAt(key) > 0 {
				// Trakt already has this viewing: don't send ours there again.
				s.db.Exec(`UPDATE plays SET trakt_at = ? WHERE user_id = ? AND item_id = ? AND ABS(stopped_at - ?) < 900 AND trakt_at = 0`,
					now(), p.UserID, p.ItemID, p.StoppedAt)
			}
			return false, nil
		}
	}
	res, err := s.db.Exec(`INSERT OR IGNORE INTO plays (user_id, item_id, title, subtitle, kind, library_id, show_id, started_at, stopped_at,
			watched, completed, mode, client, ip, import_key, trakt_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
		p.UserID, item, p.Title, p.Subtitle, p.Kind, p.LibraryID, p.ShowID, p.StartedAt, p.StoppedAt,
		p.Watched, boolInt(p.Completed), p.Mode, p.Client, key, traktAt(key))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// traktAt marks plays that came from Trakt as already on Trakt, so they aren't sent back.
func traktAt(key string) int64 {
	if strings.HasPrefix(key, "trakt:") {
		return now()
	}
	return 0
}

// ImportWatched marks an item watched. It never un-watches anything.
func (s *Store) ImportWatched(uid, itemID, at int64) error {
	_, err := s.db.Exec(`INSERT INTO watch_state (user_id, item_id, position, duration, watched, updated_at) VALUES (?, ?, 0, 0, 1, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET watched = 1, position = CASE WHEN excluded.updated_at >= watch_state.updated_at THEN 0 ELSE watch_state.position END`,
		uid, itemID, at)
	return err
}

// ImportResume sets a resume point unless BingeBandit already has a newer
// one, and reports whether it did.
func (s *Store) ImportResume(uid, itemID int64, position, duration float64, at int64) (bool, error) {
	res, err := s.db.Exec(`INSERT INTO watch_state (user_id, item_id, position, duration, watched, updated_at) VALUES (?, ?, ?, ?, 0, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET position = excluded.position, duration = excluded.duration, updated_at = excluded.updated_at
		WHERE watch_state.updated_at < excluded.updated_at`,
		uid, itemID, position, duration, at)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetCollectionArtwork replaces a collection's poster or backdrop.
func (s *Store) SetCollectionArtwork(id int64, kind, ref string) error {
	if kind != "poster" && kind != "backdrop" {
		return fmt.Errorf("unknown collection artwork type %q", kind)
	}
	_, err := s.db.Exec(`UPDATE collections SET `+kind+` = ? WHERE id = ?`, ref, id)
	return err
}

// EnsureCollection finds a collection by name in a library, creating it if needed,
// and adds the items. It returns its id and how many items were new to it.
func (s *Store) EnsureCollection(libraryID int64, name, overview string, itemIDs []int64) (id int64, added int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO collections (library_id, name, overview, created_at) VALUES (?, ?, ?, ?)`,
		libraryID, name, overview, now()); err != nil {
		return 0, 0, err
	}
	if err := tx.QueryRow(`SELECT id FROM collections WHERE library_id = ? AND name = ? COLLATE NOCASE`, libraryID, name).Scan(&id); err != nil {
		return 0, 0, err
	}
	for _, it := range itemIDs {
		res, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id) VALUES (?, ?)`, id, it)
		if err != nil {
			return 0, 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	return id, added, tx.Commit()
}

// SetCollectionLabel makes a collection gather everything with label, unless
// it already has a label rule.
func (s *Store) SetCollectionLabel(id int64, label string) error {
	_, err := s.db.Exec(`UPDATE collections SET label_rule = ? WHERE id = ? AND label_rule = ''`, label, id)
	return err
}

// AddItemLabel puts a label on a movie or show, reporting whether it was new.
func (s *Store) AddItemLabel(itemID int64, label string) (bool, error) {
	var raw string
	if err := s.db.QueryRow(`SELECT labels FROM items WHERE id = ?`, itemID).Scan(&raw); err != nil {
		return false, err
	}
	var labels []string
	json.Unmarshal([]byte(raw), &labels)
	for _, l := range labels {
		if strings.EqualFold(l, label) {
			return false, nil
		}
	}
	b, _ := json.Marshal(append(labels, label))
	_, err := s.db.Exec(`UPDATE items SET labels = ? WHERE id = ?`, string(b), itemID)
	return err == nil, err
}
