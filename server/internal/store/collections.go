package store

import (
	"database/sql"
	"encoding/json"
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
	// Mode overrides the library's Collection Mode for this collection:
	// "" (library default), "show", "hide" or "collapse".
	Mode string `json:"mode"`
	// Sort orders the titles: "" (release date), "release_desc", "title",
	// "added" or "rating". Only filled by Collection.
	Sort string `json:"sort"`
	// SortTitle orders the collection among others; "" uses its name.
	// Only filled by Collection.
	SortTitle string `json:"sortTitle,omitempty"`
	// ListURL is an MDBList list or RSS feed the collection follows.
	ListURL      string `json:"listUrl,omitempty"`
	ListSyncedAt int64  `json:"listSyncedAt,omitempty"`
	ListError    string `json:"listError,omitempty"`
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

const collectionSelect = `SELECT c.id, c.library_id, c.name, c.overview, c.poster, c.backdrop, c.tmdb_id, c.mode, c.list_url, c.list_synced_at, c.list_error, c.label_rule, c.genre_rule, c.studio_rule, c.year_from, c.year_to,
	(SELECT COUNT(*) FROM items i WHERE ` + memberOf + `),
	COALESCE((SELECT i.poster FROM items i WHERE ` + memberOf + ` AND i.poster != '' ORDER BY i.release_date LIMIT 1), '')
FROM collections c`

func scanCollection(r interface{ Scan(...any) error }) (*Collection, error) {
	var c Collection
	var fallback string
	err := r.Scan(&c.ID, &c.LibraryID, &c.Name, &c.Overview, &c.Poster, &c.Backdrop, &c.TMDBID, &c.Mode, &c.ListURL, &c.ListSyncedAt, &c.ListError, &c.LabelRule, &c.GenreRule, &c.StudioRule, &c.YearFrom, &c.YearTo, &c.ItemCount, &fallback)
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

// LibraryCollections lists a library's collections with their sizes. Members
// are worked out here in one pass over the library rather than one SQL scan
// per collection, which took seconds on libraries with many collections.
func (s *Store) LibraryCollections(libraryID int64) ([]*Collection, error) {
	cols, _, err := s.LibraryCollectionsHiding(libraryID, func(*Collection) bool { return false })
	return cols, err
}

// CollectedItems are the ids of a library's movies or shows that are in at
// least one of its collections.
func (s *Store) CollectedItems(libraryID int64) ([]int64, error) {
	_, ids, err := s.LibraryCollectionsHiding(libraryID, func(*Collection) bool { return true })
	return ids, err
}

// LibraryCollectionsHiding lists a library's collections, and the titles in
// the ones collapse picks, which the Library tab leaves out ("Hide items in
// this collection").
func (s *Store) LibraryCollectionsHiding(libraryID int64, collapse func(*Collection) bool) ([]*Collection, []int64, error) {
	cols, in, err := s.libraryCollectionMembers(libraryID, collapse)
	ids := make([]int64, 0, len(in))
	for id := range in {
		ids = append(ids, id)
	}
	return cols, ids, err
}

func (s *Store) libraryCollectionMembers(libraryID int64, collapse func(*Collection) bool) ([]*Collection, map[int64]bool, error) {
	collected := map[int64]bool{}
	cols, err := s.queryCollections(`SELECT c.id, c.library_id, c.name, c.overview, c.poster, c.backdrop, c.tmdb_id, c.mode, c.list_url, c.list_synced_at, c.list_error, c.label_rule, c.genre_rule, c.studio_rule, c.year_from, c.year_to, 0, ''
		FROM collections c WHERE c.library_id = ? ORDER BY `+collectionOrder, libraryID)
	if err != nil || len(cols) == 0 {
		return cols, collected, err
	}
	type member struct {
		labels, genres []string
		studio, poster string
		release        string
		year           int
	}
	items := map[int64]*member{}
	rows, err := s.db.Query(`SELECT id, labels, genres, studio, year, poster, release_date FROM items
		WHERE library_id = ? AND kind IN ('movie','show')`, libraryID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var labels, genres string
		m := &member{}
		if err := rows.Scan(&id, &labels, &genres, &m.studio, &m.year, &m.poster, &m.release); err != nil {
			return nil, nil, err
		}
		json.Unmarshal([]byte(labels), &m.labels)
		json.Unmarshal([]byte(genres), &m.genres)
		items[id] = m
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	explicit := map[int64]map[int64]bool{}
	xr, err := s.db.Query(`SELECT x.collection_id, x.item_id FROM collection_items x JOIN collections c ON c.id = x.collection_id WHERE c.library_id = ?`, libraryID)
	if err != nil {
		return nil, nil, err
	}
	defer xr.Close()
	for xr.Next() {
		var c, i int64
		if err := xr.Scan(&c, &i); err != nil {
			return nil, nil, err
		}
		if explicit[c] == nil {
			explicit[c] = map[int64]bool{}
		}
		explicit[c][i] = true
	}
	if err := xr.Err(); err != nil {
		return nil, nil, err
	}
	has := func(list []string, v string) bool {
		for _, x := range list {
			if strings.EqualFold(x, v) {
				return true
			}
		}
		return false
	}
	for _, c := range cols {
		smart, hides := c.Smart(), collapse(c)
		var first *member
		for id, m := range items {
			in := explicit[c.ID][id]
			if !in && smart {
				in = (c.LabelRule == "" || has(m.labels, c.LabelRule)) &&
					(c.GenreRule == "" || has(m.genres, c.GenreRule)) &&
					(c.StudioRule == "" || strings.EqualFold(m.studio, c.StudioRule)) &&
					(c.YearFrom == 0 || m.year >= c.YearFrom) &&
					(c.YearTo == 0 || (m.year > 0 && m.year <= c.YearTo))
			}
			if !in {
				continue
			}
			c.ItemCount++
			if hides {
				collected[id] = true
			}
			if m.poster != "" && (first == nil || m.release < first.release) {
				first = m
			}
		}
		if c.Poster == "" && first != nil {
			c.Poster = ImageURL(first.poster)
		}
	}
	return cols, collected, nil
}

func (s *Store) Collection(id int64) (*Collection, error) {
	c, err := scanCollection(s.db.QueryRow(collectionSelect+` WHERE c.id = ?`, id))
	if err == nil {
		c.Sort = s.collectionSort(id)
		s.db.QueryRow(`SELECT sort_title FROM collections WHERE id = ?`, id).Scan(&c.SortTitle)
	}
	return c, err
}

// CollectionSorts maps each collection sort to its ORDER BY.
var CollectionSorts = map[string]string{
	"":             `i.release_date, i.sort_title COLLATE NOCASE, i.id`,
	"release_desc": `i.release_date DESC, i.sort_title COLLATE NOCASE, i.id`,
	"title":        `i.sort_title COLLATE NOCASE, i.year, i.id`,
	"added":        `i.added_at DESC, i.id DESC`,
	"rating":       `i.rating DESC, i.sort_title COLLATE NOCASE, i.id`,
}

func (s *Store) collectionSort(id int64) string {
	var sort string
	s.db.QueryRow(`SELECT sort FROM collections WHERE id = ?`, id).Scan(&sort)
	if _, ok := CollectionSorts[sort]; !ok {
		return ""
	}
	return sort
}

// SetCollectionSortTitle sets the title a collection is ordered by; "" uses its name.
func (s *Store) SetCollectionSortTitle(id int64, sortTitle string) error {
	_, err := s.db.Exec(`UPDATE collections SET sort_title = ? WHERE id = ?`, sortTitle, id)
	return err
}

// collectionOrder lists collections by sort title, falling back to the name.
const collectionOrder = `COALESCE(NULLIF(c.sort_title, ''), c.name) COLLATE NOCASE, c.name COLLATE NOCASE`

// SetCollectionSort changes how a collection's titles are ordered.
func (s *Store) SetCollectionSort(id int64, sort string) error {
	_, err := s.db.Exec(`UPDATE collections SET sort = ? WHERE id = ?`, sort, id)
	return err
}

// ItemCollections lists every collection an item is in, explicit or by label.
func (s *Store) ItemCollections(itemID int64) ([]*Collection, error) {
	return s.queryCollections(collectionSelect+` WHERE EXISTS (SELECT 1 FROM items i WHERE i.id = ? AND `+memberOf+`)
		ORDER BY `+collectionOrder, itemID)
}

// CollectionMembers lists every title in a collection, in its sort order.
func (s *Store) CollectionMembers(uid, collectionID int64) ([]*Item, error) {
	return s.CollectionPage(uid, collectionID, 0, -1)
}

// CollectionPage is a page of a collection's titles; a limit of -1 is all of them.
func (s *Store) CollectionPage(uid, collectionID int64, offset, limit int) ([]*Item, error) {
	return s.pagedItems(`JOIN collections c ON c.id = @c`, memberOf+` AND `+visible,
		CollectionSorts[s.collectionSort(collectionID)],
		uidArg(uid), sql.Named("c", collectionID), sql.Named("offset", offset), sql.Named("limit", limit))
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

func (s *Store) UpdateCollection(id int64, name, overview, mode string, rules SmartRules) error {
	_, err := s.db.Exec(`UPDATE collections SET name = ?, overview = ?, mode = ?, label_rule = ?, genre_rule = ?, studio_rule = ?, year_from = ?, year_to = ?
		WHERE id = ?`, name, overview, mode, rules.LabelRule, rules.GenreRule, rules.StudioRule, rules.YearFrom, rules.YearTo, id)
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

// SetCollectionList makes a collection follow a list ("" stops following it,
// keeping the members it has).
func (s *Store) SetCollectionList(id int64, url string) error {
	_, err := s.db.Exec(`UPDATE collections SET list_url = ?, list_error = '' WHERE id = ?`, url, id)
	if err == nil && url == "" {
		_, err = s.db.Exec(`UPDATE collection_items SET from_list = 0 WHERE collection_id = ?`, id)
	}
	return err
}

// ListCollections are the collections that follow a list.
func (s *Store) ListCollections() ([]*Collection, error) {
	return s.queryCollections(`SELECT c.id, c.library_id, c.name, c.overview, c.poster, c.backdrop, c.tmdb_id, c.mode, c.list_url, c.list_synced_at, c.list_error,
		c.label_rule, c.genre_rule, c.studio_rule, c.year_from, c.year_to, 0, '' FROM collections c WHERE c.list_url != '' ORDER BY c.id`)
}

// SyncCollectionList replaces the members a list added with itemIDs. Titles
// added by hand stay. With failed set, members are kept and the error saved.
func (s *Store) SyncCollectionList(id int64, itemIDs []int64, failed string) error {
	if failed != "" {
		_, err := s.db.Exec(`UPDATE collections SET list_error = ? WHERE id = ?`, failed, id)
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM collection_items WHERE collection_id = ? AND from_list = 1`, id); err != nil {
		return err
	}
	for _, it := range itemIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id, from_list) VALUES (?, ?, 1)`, id, it); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE collections SET list_synced_at = ?, list_error = '' WHERE id = ?`, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}
