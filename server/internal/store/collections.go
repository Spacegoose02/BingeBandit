package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Collection groups movies or shows within one library. Members are linked
// explicitly (TMDB collections, admin picks) and, for smart collections,
// every item that matches all of its rules (label, genre, studio, years).
type Collection struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"libraryId"`
	Name      string `json:"name"`
	Overview  string `json:"overview,omitempty"`
	Poster    string `json:"poster,omitempty"`
	Backdrop  string `json:"backdrop,omitempty"`
	TMDBID    int64  `json:"tmdbId,omitempty"`
	SmartRules
	ItemCount int `json:"itemCount"`
	// Visible is where the collection shows as a row; only sent to admins.
	Visible *Visibility `json:"visible,omitempty"`
}

// SmartRules pick a collection's members automatically. Each rule that is set
// must match; with none set, the collection is hand-picked.
type SmartRules struct {
	LabelRule  string `json:"labelRule,omitempty"`
	GenreRule  string `json:"genreRule,omitempty"`
	StudioRule string `json:"studioRule,omitempty"` // studio for movies, network for shows
	YearFrom   int    `json:"yearFrom,omitempty"`
	YearTo     int    `json:"yearTo,omitempty"`
}

// Smart reports whether any rule is set.
func (r SmartRules) Smart() bool {
	return r.LabelRule != "" || r.GenreRule != "" || r.StudioRule != "" || r.YearFrom > 0 || r.YearTo > 0
}

// memberOf is true when item alias i belongs to collection alias c.
const memberOf = `(i.library_id = c.library_id AND i.kind IN ('movie','show') AND (
	i.id IN (SELECT x.item_id FROM collection_items x WHERE x.collection_id = c.id)
	OR ((c.label_rule != '' OR c.genre_rule != '' OR c.studio_rule != '' OR c.year_from > 0 OR c.year_to > 0)
		AND (c.label_rule = '' OR EXISTS (SELECT 1 FROM json_each(i.labels) j WHERE j.value = c.label_rule COLLATE NOCASE))
		AND (c.genre_rule = '' OR EXISTS (SELECT 1 FROM json_each(i.genres) g WHERE g.value = c.genre_rule COLLATE NOCASE))
		AND (c.studio_rule = '' OR i.studio = c.studio_rule COLLATE NOCASE)
		AND (c.year_from = 0 OR i.year >= c.year_from)
		AND (c.year_to = 0 OR (i.year > 0 AND i.year <= c.year_to)))))`

const collectionSelect = `SELECT c.id, c.library_id, c.name, c.overview, c.poster, c.backdrop, c.tmdb_id, c.label_rule, c.genre_rule, c.studio_rule, c.year_from, c.year_to,
	(SELECT COUNT(*) FROM items i WHERE ` + memberOf + `),
	COALESCE((SELECT i.poster FROM items i WHERE ` + memberOf + ` AND i.poster != '' ORDER BY i.release_date LIMIT 1), '')
FROM collections c`

func scanCollection(r interface{ Scan(...any) error }) (*Collection, error) {
	var c Collection
	var fallback string
	err := r.Scan(&c.ID, &c.LibraryID, &c.Name, &c.Overview, &c.Poster, &c.Backdrop, &c.TMDBID, &c.LabelRule, &c.GenreRule, &c.StudioRule, &c.YearFrom, &c.YearTo, &c.ItemCount, &fallback)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.Poster == "" {
		c.Poster = fallback
	}
	c.Poster, c.Backdrop = ImageURL(c.Poster), ImageURL(c.Backdrop)
	return &c, nil
}

func (s *Store) queryCollections(q string, args ...any) ([]*Collection, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Collection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) LibraryCollections(libraryID int64) ([]*Collection, error) {
	return s.queryCollections(collectionSelect+` WHERE c.library_id = ? ORDER BY c.name COLLATE NOCASE`, libraryID)
}

func (s *Store) Collection(id int64) (*Collection, error) {
	return scanCollection(s.db.QueryRow(collectionSelect+` WHERE c.id = ?`, id))
}

// ItemCollections lists every collection an item is in, explicit or by label.
func (s *Store) ItemCollections(itemID int64) ([]*Collection, error) {
	return s.queryCollections(collectionSelect+` WHERE EXISTS (SELECT 1 FROM items i WHERE i.id = ? AND `+memberOf+`)
		ORDER BY c.name COLLATE NOCASE`, itemID)
}

func (s *Store) CollectionMembers(uid, collectionID int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` JOIN collections c ON c.id = @c WHERE `+memberOf+` AND `+visible+`
		ORDER BY i.release_date, i.sort_title COLLATE NOCASE`, uidArg(uid), sql.Named("c", collectionID))
}

// collectionByName finds or creates a collection in a library.
func collectionByName(tx *sql.Tx, libraryID int64, name string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM collections WHERE library_id = ? AND name = ? COLLATE NOCASE`, libraryID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.Exec(`INSERT INTO collections (library_id, name, created_at) VALUES (?, ?, ?)`, libraryID, name, now())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	return id, err
}

// LinkTMDBCollection adds a movie to its TMDB collection, creating the
// collection on first sight. Skipped when the item's collections are locked.
func (s *Store) LinkTMDBCollection(itemID, libraryID, tmdbID int64, name, poster, backdrop string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked string
	if err := tx.QueryRow(`SELECT locked FROM items WHERE id = ?`, itemID).Scan(&locked); err != nil {
		return err
	}
	if strings.Contains(locked, `"collections"`) {
		return nil
	}
	var id int64
	err = tx.QueryRow(`SELECT id FROM collections WHERE library_id = ? AND tmdb_id = ?`, libraryID, tmdbID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if id, err = collectionByName(tx, libraryID, name); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE collections SET tmdb_id = ? WHERE id = ?`, tmdbID, id); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE collections SET poster = CASE WHEN poster = '' THEN ? ELSE poster END,
		backdrop = CASE WHEN backdrop = '' THEN ? ELSE backdrop END WHERE id = ?`, poster, backdrop, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id) VALUES (?, ?)`, id, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetItemCollections replaces an item's explicit collection links by name,
// creating collections as needed. Label-based membership is unaffected.
func (s *Store) SetItemCollections(itemID, libraryID int64, names []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM collection_items WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		id, err := collectionByName(tx, libraryID, n)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id) VALUES (?, ?)`, id, itemID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CreateCollection(libraryID int64, name string, rules SmartRules) (*Collection, error) {
	res, err := s.db.Exec(`INSERT INTO collections (library_id, name, label_rule, genre_rule, studio_rule, year_from, year_to, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID, name, rules.LabelRule, rules.GenreRule, rules.StudioRule, rules.YearFrom, rules.YearTo, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Collection(id)
}

func (s *Store) UpdateCollection(id int64, name, overview string, rules SmartRules) error {
	_, err := s.db.Exec(`UPDATE collections SET name = ?, overview = ?, label_rule = ?, genre_rule = ?, studio_rule = ?, year_from = ?, year_to = ?
		WHERE id = ?`, name, overview, rules.LabelRule, rules.GenreRule, rules.StudioRule, rules.YearFrom, rules.YearTo, id)
	return err
}

func (s *Store) DeleteCollection(id int64) error {
	_, err := s.db.Exec(`DELETE FROM collections WHERE id = ?`, id)
	return err
}

// ItemCollectionNames lists the collections an item is explicitly linked to
// (what the edit dialog shows), excluding label-based membership.
func (s *Store) ItemCollectionNames(itemID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT c.name FROM collections c JOIN collection_items x ON x.collection_id = c.id
		WHERE x.item_id = ? ORDER BY c.name COLLATE NOCASE`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// LibraryGenres and LibraryStudios list the values used in a library, for
// smart collection rules.
func (s *Store) LibraryGenres(libraryID int64) ([]string, error) {
	return s.queryStrings(`SELECT DISTINCT j.value FROM items i, json_each(i.genres) j WHERE i.library_id = ? ORDER BY 1 COLLATE NOCASE`, libraryID)
}

func (s *Store) LibraryStudios(libraryID int64) ([]string, error) {
	return s.queryStrings(`SELECT DISTINCT studio FROM items WHERE library_id = ? AND studio != '' AND kind IN ('movie','show') ORDER BY 1 COLLATE NOCASE`, libraryID)
}

func (s *Store) queryStrings(q string, args ...any) ([]string, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LibraryLabels lists every label used in a library, for autocomplete.
func (s *Store) LibraryLabels(libraryID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT j.value FROM items i, json_each(i.labels) j WHERE i.library_id = ? ORDER BY 1 COLLATE NOCASE`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
