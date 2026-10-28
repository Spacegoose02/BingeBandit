package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"unicode/utf8"
)

// titleIDs matches a movie or show ?1 and, for a show, its seasons and episodes.
const titleIDs = `(SELECT id FROM items WHERE id = ?1 OR show_id = ?1)`

// TitlePaths lists the files of a movie, or of every episode of a show.
func (s *Store) TitlePaths(id int64) ([]string, error) {
	return s.queryStrings(`SELECT path FROM media_files WHERE item_id IN `+titleIDs+` ORDER BY path`, id)
}

// OthersUnder says whether a folder (ending in a path separator) holds files
// of titles other than id, so it can't be moved along with id.
func (s *Store) OthersUnder(id int64, prefix string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE substr(path, 1, ?2) = ?3 AND item_id NOT IN `+titleIDs,
		id, utf8.RuneCountInString(prefix), prefix).Scan(&n)
	return n > 0, err
}

// MatchKey is the key scans find an item by.
func (s *Store) MatchKey(id int64) (string, error) {
	var k string
	err := s.db.QueryRow(`SELECT match_key FROM items WHERE id = ?`, id).Scan(&k)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return k, err
}

// KeyTaken says whether a library already has a title with this match key.
func (s *Store) KeyTaken(libraryID int64, key string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_id = ? AND match_key = ?`, libraryID, key).Scan(&n)
	return n > 0, err
}

// MoveTitle puts a movie or show (with its seasons and episodes) in another
// library once its files have moved on disk, keeping its id and so its
// watch state, artwork, markers and kid approvals. paths maps each moved
// file or folder to where it went. A show's match keys change from oldKey to
// newKey (a folder's key depends on the other folders around it). Its place
// in the old library's collections carries over to same-named (or same TMDB)
// collections in the new one, made if needed; built-in and list-synced ones
// are left to their own syncs.
func (s *Store) MoveTitle(id, toLibrary int64, oldKey, newKey string, paths map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE items SET library_id = ?2,
			match_key = CASE WHEN substr(match_key, 1, ?3) = ?4 THEN ?5 || substr(match_key, ?3 + 1) ELSE match_key END
		WHERE id IN `+titleIDs, id, toLibrary, utf8.RuneCountInString(oldKey), oldKey, newKey); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE item_aliases SET library_id = ?2 WHERE item_id IN `+titleIDs, id, toLibrary); err != nil {
		return err
	}
	sep := string(filepath.Separator)
	for from, to := range paths {
		// The folder itself never is a file, so this only matches inside it.
		if _, err := tx.Exec(`UPDATE media_files SET path = CASE WHEN path = ?1 THEN ?2 ELSE ?2 || ?5 || substr(path, ?3 + 1) END
			WHERE path = ?1 OR substr(path, 1, ?3) = ?4`,
			from, to, utf8.RuneCountInString(from+sep), from+sep, sep); err != nil {
			return err
		}
	}
	if err := moveCollections(tx, id, toLibrary); err != nil {
		return err
	}
	return tx.Commit()
}

func moveCollections(tx *sql.Tx, id, toLibrary int64) error {
	type link struct {
		collection, item, tmdb int64
		name, poster, backdrop string
		keep                   bool
	}
	rows, err := tx.Query(`SELECT c.id, ci.item_id, c.tmdb_id, c.name, c.poster, c.backdrop, c.builtin = '' AND ci.from_list = 0
		FROM collection_items ci JOIN collections c ON c.id = ci.collection_id
		WHERE ci.item_id IN `+titleIDs+` AND c.library_id != ?2`, id, toLibrary)
	if err != nil {
		return err
	}
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.collection, &l.item, &l.tmdb, &l.name, &l.poster, &l.backdrop, &l.keep); err != nil {
			rows.Close()
			return err
		}
		links = append(links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec(`DELETE FROM collection_items WHERE collection_id = ? AND item_id = ?`, l.collection, l.item); err != nil {
			return err
		}
		if !l.keep {
			continue
		}
		var to int64
		err := sql.ErrNoRows
		if l.tmdb > 0 {
			err = tx.QueryRow(`SELECT id FROM collections WHERE library_id = ? AND tmdb_id = ?`, toLibrary, l.tmdb).Scan(&to)
		}
		if errors.Is(err, sql.ErrNoRows) {
			if to, err = collectionByName(tx, toLibrary, l.name); err != nil {
				return err
			}
			_, err = tx.Exec(`UPDATE collections SET
				tmdb_id = CASE WHEN tmdb_id = 0 THEN ? ELSE tmdb_id END,
				poster = CASE WHEN poster = '' THEN ? ELSE poster END,
				backdrop = CASE WHEN backdrop = '' THEN ? ELSE backdrop END WHERE id = ?`, l.tmdb, l.poster, l.backdrop, to)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id) VALUES (?, ?)`, to, l.item); err != nil {
			return err
		}
	}
	return nil
}
