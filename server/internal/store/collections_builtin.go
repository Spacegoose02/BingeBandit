package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// Default collections (Kometa's defaults) carry a builtin key, such as
// studio:Pixar, decade:1980, genre:Comedy or award:oscar-best-picture.

// BuiltinCollections maps a library's default collections' keys to their ids.
func (s *Store) BuiltinCollections(libraryID int64) (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT builtin, id FROM collections WHERE library_id = ? AND builtin != ''`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var id int64
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		out[k] = id
	}
	return out, rows.Err()
}

// CreateBuiltinCollection makes a default collection. They start with
// Collection Mode "hide", so the Library tab keeps showing the titles
// rather than a tile per studio, decade and genre. A name an admin's own
// collection already has returns ErrExists, and theirs is left alone.
func (s *Store) CreateBuiltinCollection(libraryID int64, key, name, overview string, rules SmartRules) (int64, error) {
	var taken int
	s.db.QueryRow(`SELECT COUNT(*) FROM collections WHERE library_id = ? AND name = ? COLLATE NOCASE`, libraryID, name).Scan(&taken)
	if taken > 0 {
		return 0, ErrExists
	}
	res, err := s.db.Exec(`INSERT INTO collections (library_id, name, overview, mode, builtin, label_rule, genre_rule, studio_rule, year_from, year_to, created_at)
		VALUES (?, ?, ?, 'hide', ?, ?, ?, ?, ?, ?, ?)`,
		libraryID, name, overview, key, rules.LabelRule, rules.GenreRule, rules.StudioRule, rules.YearFrom, rules.YearTo, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetBuiltinMembers replaces a default collection's members (award winners)
// in the order given.
func (s *Store) SetBuiltinMembers(id int64, itemIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM collection_items WHERE collection_id = ?`, id); err != nil {
		return err
	}
	for i, it := range itemIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO collection_items (collection_id, item_id, from_list) VALUES (?, ?, ?)`, id, it, i+1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Facet is a studio, genre or decade and how many of a library's titles have it.
type Facet struct {
	Value string
	Count int
}

// LibraryFacetCounts counts a library's movies or shows by studio (network
// for shows), genre and decade ("1980").
func (s *Store) LibraryFacetCounts(libraryID int64) (studios, genres, decades []Facet, err error) {
	q := func(query string) ([]Facet, error) {
		rows, err := s.db.Query(query, libraryID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Facet
		for rows.Next() {
			var f Facet
			if err := rows.Scan(&f.Value, &f.Count); err != nil {
				return nil, err
			}
			out = append(out, f)
		}
		return out, rows.Err()
	}
	if studios, err = q(`SELECT MIN(studio), COUNT(*) FROM items WHERE library_id = ? AND kind IN ('movie','show') AND studio != ''
		GROUP BY studio COLLATE NOCASE ORDER BY COUNT(*) DESC, MIN(studio)`); err != nil {
		return
	}
	if genres, err = q(`SELECT MIN(g.value), COUNT(DISTINCT i.id) FROM items i, json_each(i.genres) g
		WHERE i.library_id = ? AND i.kind IN ('movie','show') AND g.value != ''
		GROUP BY g.value COLLATE NOCASE ORDER BY COUNT(DISTINCT i.id) DESC`); err != nil {
		return
	}
	decades, err = q(`SELECT CAST((year / 10) * 10 AS TEXT), COUNT(*) FROM items WHERE library_id = ? AND kind IN ('movie','show') AND year > 0
		GROUP BY year / 10 ORDER BY year / 10`)
	return
}

// ExternalRef names a title by IMDb id, TMDB id or both.
type ExternalRef struct {
	IMDb string
	TMDB int64
}

// ItemsByExternalIDs finds a library's movies or shows (kind) by IMDb or
// TMDB id, each once, in the order of refs.
func (s *Store) ItemsByExternalIDs(libraryID int64, kind string, refs []ExternalRef) ([]int64, error) {
	var imdb []string
	var tmdb []int64
	for _, r := range refs {
		if r.IMDb != "" {
			imdb = append(imdb, r.IMDb)
		}
		if r.TMDB > 0 {
			tmdb = append(tmdb, r.TMDB)
		}
	}
	im, _ := json.Marshal(append([]string{}, imdb...))
	tm, _ := json.Marshal(append([]int64{}, tmdb...))
	rows, err := s.db.Query(`SELECT i.id, i.imdb_id, i.tmdb_id FROM items i WHERE i.library_id = @lib AND i.kind = @kind AND (
		(i.imdb_id != '' AND i.imdb_id IN (SELECT value FROM json_each(@imdb)))
		OR (i.tmdb_id > 0 AND i.tmdb_id IN (SELECT value FROM json_each(@tmdb))))`,
		sql.Named("lib", libraryID), sql.Named("kind", kind), sql.Named("imdb", string(im)), sql.Named("tmdb", string(tm)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byIMDb, byTMDB := map[string]int64{}, map[int64]int64{}
	for rows.Next() {
		var id, t int64
		var i string
		if err := rows.Scan(&id, &i, &t); err != nil {
			return nil, err
		}
		if i != "" {
			byIMDb[strings.ToLower(i)] = id
		}
		if t > 0 {
			byTMDB[t] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var out []int64
	for _, r := range refs {
		id := byIMDb[strings.ToLower(r.IMDb)]
		if id == 0 && r.TMDB > 0 {
			id = byTMDB[r.TMDB]
		}
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}
