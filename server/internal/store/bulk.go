package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// BulkEdit is one change applied to many movies, shows or collections at once
// (the web's multi-select editor). Names and labels match without case.
type BulkEdit struct {
	AddCollections    []string `json:"addCollections"`
	RemoveCollections []string `json:"removeCollections"`
	AddLabels         []string `json:"addLabels"`
	RemoveLabels      []string `json:"removeLabels"`
}

// editLabels adds and removes labels on a JSON list of them, keeping the
// order of the ones that stay.
func editLabels(raw string, add, remove []string) (string, bool) {
	var labels []string
	json.Unmarshal([]byte(raw), &labels)
	out := []string{}
	for _, l := range labels {
		if !hasFold(remove, l) {
			out = append(out, l)
		}
	}
	for _, l := range add {
		if !hasFold(out, l) {
			out = append(out, l)
		}
	}
	b, _ := json.Marshal(out)
	return string(b), string(b) != raw
}

func hasFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// BulkEditItems puts movies and shows in or takes them out of collections (by
// name, in each title's own library, making new ones as needed) and adds or
// removes their labels. Seasons and episodes are skipped. It returns how many
// titles it changed.
func (s *Store) BulkEditItems(ids []int64, e BulkEdit) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	changed := 0
	for _, id := range ids {
		var libraryID int64
		var raw string
		err := tx.QueryRow(`SELECT library_id, labels FROM items WHERE id = ? AND kind IN ('movie','show')`, id).Scan(&libraryID, &raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return 0, err
		}
		did := false
		if labels, diff := editLabels(raw, e.AddLabels, e.RemoveLabels); diff {
			if _, err := tx.Exec(`UPDATE items SET labels = ? WHERE id = ?`, labels, id); err != nil {
				return 0, err
			}
			did = true
		}
		for _, n := range e.AddCollections {
			cid, err := collectionByName(tx, libraryID, n)
			if err != nil {
				return 0, err
			}
			res, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id) VALUES (?, ?)`, cid, id)
			if err != nil {
				return 0, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				did = true
			}
		}
		for _, n := range e.RemoveCollections {
			res, err := tx.Exec(`DELETE FROM collection_items WHERE item_id = ? AND collection_id IN
				(SELECT id FROM collections WHERE library_id = ? AND name = ? COLLATE NOCASE)`, id, libraryID, n)
			if err != nil {
				return 0, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				did = true
			}
		}
		if did {
			changed++
		}
	}
	return changed, tx.Commit()
}

// BulkEditCollections adds or removes labels on collections. Collections
// only carry labels, so the collection fields of e are ignored.
func (s *Store) BulkEditCollections(ids []int64, e BulkEdit) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	changed := 0
	for _, id := range ids {
		var raw string
		err := tx.QueryRow(`SELECT labels FROM collections WHERE id = ?`, id).Scan(&raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return 0, err
		}
		if labels, diff := editLabels(raw, e.AddLabels, e.RemoveLabels); diff {
			if _, err := tx.Exec(`UPDATE collections SET labels = ? WHERE id = ?`, labels, id); err != nil {
				return 0, err
			}
			changed++
		}
	}
	return changed, tx.Commit()
}

// SetCollectionLabels replaces a collection's labels.
func (s *Store) SetCollectionLabels(id int64, labels []string) error {
	b, _ := json.Marshal(nonNil(labels))
	_, err := s.db.Exec(`UPDATE collections SET labels = ? WHERE id = ?`, string(b), id)
	return err
}

// CollectionLabels lists every label used on a library's collections, for
// autocomplete; libraryID 0 means every library.
func (s *Store) CollectionLabels(libraryID int64) ([]string, error) {
	return s.queryStrings(`SELECT DISTINCT j.value FROM collections c, json_each(c.labels) j
		WHERE ? = 0 OR c.library_id = ? ORDER BY 1 COLLATE NOCASE`, libraryID, libraryID)
}
