package store

import (
	"path/filepath"
	"unicode/utf8"
)

// treeIDs matches item ?1 and everything below it: a show's seasons and
// episodes, or a season's episodes.
const treeIDs = `(SELECT id FROM items WHERE id = ?1 OR parent_id = ?1 OR show_id = ?1)`

// ItemPaths lists the files of a movie or episode, or of every episode of a
// season or show, counting multi-episode files any of them play.
func (s *Store) ItemPaths(id int64) ([]string, error) {
	return s.queryStrings(`SELECT path FROM media_files WHERE item_id IN `+treeIDs+`
		OR id IN (SELECT file_id FROM media_file_episodes WHERE item_id IN `+treeIDs+`) ORDER BY path`, id)
}

// OthersUnderTree is OthersUnder for any item and what's below it.
func (s *Store) OthersUnderTree(id int64, prefix string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE substr(path, 1, ?2) = ?3 AND item_id NOT IN `+treeIDs,
		id, utf8.RuneCountInString(prefix), prefix).Scan(&n)
	return n > 0, err
}

// ForgetPaths drops a library's files at the given paths, or below them for
// folders, once they're gone from disk, then the movies, episodes, seasons
// and shows left with nothing, as a scan would.
func (s *Store) ForgetPaths(libraryID int64, paths []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sep := string(filepath.Separator)
	for _, p := range paths {
		if _, err := tx.Exec(`DELETE FROM media_files WHERE (path = ?1 OR substr(path, 1, ?2) = ?3)
			AND item_id IN (SELECT id FROM items WHERE library_id = ?4)`,
			p, utf8.RuneCountInString(p+sep), p+sep, libraryID); err != nil {
			return err
		}
	}
	if err := deleteOrphans(tx, libraryID); err != nil {
		return err
	}
	return tx.Commit()
}
