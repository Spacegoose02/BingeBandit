package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Visibility says where a library row shows: the library's Recommended tab,
// Home, and Home for people outside the admin's household (Plex's "Friends'
// Home").
type Visibility struct {
	Library bool `json:"library"`
	Home    bool `json:"home"`
	Friends bool `json:"friends"`
}

// LibraryRow is one row a library can show, in the library's order.
type LibraryRow struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Visibility
	// LibraryOnly rows (Continue Watching) have no Home version: Home has its own.
	LibraryOnly  bool  `json:"libraryOnly,omitempty"`
	CollectionID int64 `json:"collectionId,omitempty"`
}

type builtinRow struct {
	key, movies, shows string
	def                Visibility
	libraryOnly        bool
}

// builtinRows are the rows every library has, in their default order.
var builtinRows = []builtinRow{
	{"continue", "Continue Watching", "Continue Watching", Visibility{true, false, false}, true},
	{"recent", "Recently Added Movies", "Recently Added TV", Visibility{true, true, true}, false},
	{"released", "Recently Released Movies", "Recently Released TV", Visibility{true, true, true}, false},
	{"top-rated", "Top Rated Movies", "Top Rated TV", Visibility{true, false, false}, false},
	{"unwatched", "Top Unwatched Movies", "Top Unwatched TV", Visibility{true, false, false}, false},
	{"genres", "Top Movies in (Genre)", "Top TV in (Genre)", Visibility{true, false, false}, false},
	{"watched", "Recently Watched Movies", "Recently Watched TV", Visibility{false, false, false}, false},
}

// CollectionRowKey is the row key of a collection.
func CollectionRowKey(id int64) string { return "collection:" + strconv.FormatInt(id, 10) }

// RowTitle is a built-in row's title for a library kind, or "" for unknown keys.
func RowTitle(key, libraryKind string) string {
	for _, b := range builtinRows {
		if b.key == key {
			if libraryKind == "shows" {
				return b.shows
			}
			return b.movies
		}
	}
	return ""
}

// LibraryRows lists every row lib can show, in order: rows the admin saved,
// then any built-in rows and collections they never placed, with defaults.
// Collections start hidden everywhere until an admin turns them on.
func (s *Store) LibraryRows(lib *Library) ([]LibraryRow, error) {
	// Only names: counting every collection's members is slow on big
	// libraries, and Home builds these rows for every pinned library.
	cols, err := s.collectionNames(lib.ID)
	if err != nil {
		return nil, err
	}
	all := map[string]LibraryRow{}
	var order []string
	for _, b := range builtinRows {
		r := LibraryRow{Key: b.key, Title: RowTitle(b.key, lib.Kind), Visibility: b.def, LibraryOnly: b.libraryOnly}
		if r.LibraryOnly {
			r.Home, r.Friends = false, false
		}
		all[r.Key] = r
		order = append(order, r.Key)
	}
	for _, c := range cols {
		k := CollectionRowKey(c.id)
		all[k] = LibraryRow{Key: k, Title: c.name, CollectionID: c.id}
		order = append(order, k)
	}

	rows, err := s.db.Query(`SELECT row_key, on_library, on_home, on_friends FROM library_rows
		WHERE library_id = ? ORDER BY position`, lib.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryRow{}
	placed := map[string]bool{}
	for rows.Next() {
		var key string
		var v Visibility
		if err := rows.Scan(&key, &v.Library, &v.Home, &v.Friends); err != nil {
			return nil, err
		}
		r, ok := all[key]
		if !ok || placed[key] {
			continue // a deleted collection
		}
		r.Visibility = v
		if r.LibraryOnly {
			r.Home, r.Friends = false, false
		}
		out = append(out, r)
		placed[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, k := range order {
		if !placed[k] {
			out = append(out, all[k])
		}
	}
	return out, nil
}

// SetLibraryRows saves a library's rows in order. Keys it doesn't know are
// dropped; rows left out go back to their defaults.
func (s *Store) SetLibraryRows(libraryID int64, rows []LibraryRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM library_rows WHERE library_id = ?`, libraryID); err != nil {
		return err
	}
	for i, r := range rows {
		if RowTitle(r.Key, "") == "" && !strings.HasPrefix(r.Key, "collection:") {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO library_rows (library_id, row_key, position, on_library, on_home, on_friends)
			VALUES (?, ?, ?, ?, ?, ?)`, libraryID, r.Key, i, r.Library, r.Home, r.Friends); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetRowVisibility changes where one row shows, keeping the library's order.
func (s *Store) SetRowVisibility(lib *Library, key string, v Visibility) error {
	rows, err := s.LibraryRows(lib)
	if err != nil {
		return err
	}
	found := false
	for i := range rows {
		if rows[i].Key == key {
			rows[i].Visibility = v
			found = true
		}
	}
	if !found {
		return ErrNotFound
	}
	return s.SetLibraryRows(lib.ID, rows)
}

// RowItems fills a built-in row from the given libraries (all of one kind).
// For "genres" pass the genre; TopGenres picks which.
func (s *Store) RowItems(uid int64, libs []int64, libraryKind, key, genre string, limit int) ([]*Item, error) {
	kind := "movie"
	if libraryKind == "shows" {
		kind = "show"
	}
	ids, _ := json.Marshal(libs)
	where := `i.kind = @kind AND i.library_id IN (SELECT value FROM json_each(@libs)) AND ` + visible
	args := []any{uidArg(uid), sql.Named("kind", kind), sql.Named("libs", string(ids)), sql.Named("limit", limit), sql.Named("offset", 0)}
	var q, order string
	switch key {
	case "recent":
		args = append(args, sql.Named("since", s.recentSince()))
		q, order = where+` AND `+addedAt(kind)+` >= @since`, addedAt(kind)+` DESC`
	case "released":
		args = append(args, sql.Named("today", time.Now().Format("2006-01-02")))
		q, order = where+` AND i.release_date != '' AND i.release_date <= @today`, `i.release_date DESC`
	case "top-rated":
		q, order = where+` AND i.rating > 0`, `i.rating DESC, i.sort_title COLLATE NOCASE`
	case "unwatched":
		q, order = where+` AND i.rating > 0 AND `+unwatched, `i.rating DESC, i.sort_title COLLATE NOCASE`
	case "genres":
		args = append(args, sql.Named("genre", genre))
		q, order = where+` AND EXISTS (SELECT 1 FROM json_each(i.genres) g WHERE g.value = @genre)`, `i.rating DESC, i.sort_title COLLATE NOCASE`
	case "watched":
		q, order = where+` AND `+lastWatched+` > 0`, lastWatched+` DESC`
	case "continue":
		// Movies in progress, and shows with an episode in progress or
		// started but not finished.
		q = where + ` AND ((i.kind = 'movie' AND w.position > 15 AND COALESCE(w.watched, 0) = 0)
			OR (i.kind = 'show' AND EXISTS (SELECT 1 FROM watch_state x JOIN items e ON e.id = x.item_id
				WHERE e.show_id = i.id AND x.user_id = @uid AND (x.watched = 1 OR x.position > 15)) AND ` + unwatchedEpisodes + `))`
		order = lastWatched + ` DESC`
	default:
		return nil, fmt.Errorf("unknown row %q", key)
	}
	return s.pagedItems("", q, order+`, i.id`, args...)
}

// unwatched: a movie not watched, or a show with no episode watched.
const unwatched = `((i.kind = 'movie' AND COALESCE(w.watched, 0) = 0) OR (i.kind = 'show' AND NOT EXISTS (SELECT 1 FROM watch_state x
	JOIN items e ON e.id = x.item_id WHERE e.show_id = i.id AND x.user_id = @uid AND x.watched = 1)))`

const unwatchedEpisodes = `EXISTS (SELECT 1 FROM items e WHERE e.show_id = i.id AND e.kind = 'episode'
	AND NOT EXISTS (SELECT 1 FROM watch_state x WHERE x.item_id = e.id AND x.user_id = @uid AND x.watched = 1))`

// lastWatched is when the user last watched the movie or any of the show's episodes.
const lastWatched = `(CASE i.kind WHEN 'movie' THEN COALESCE(w.updated_at, 0)
	ELSE COALESCE((SELECT MAX(x.updated_at) FROM watch_state x JOIN items e ON e.id = x.item_id
		WHERE e.show_id = i.id AND x.user_id = @uid AND (x.watched = 1 OR x.position > 15)), 0) END)`

// TopGenres are the genres with the most titles across libs.
func (s *Store) TopGenres(libs []int64, n int) ([]string, error) {
	ids, _ := json.Marshal(libs)
	rows, err := s.db.Query(`SELECT g.value FROM items i, json_each(i.genres) g
		WHERE i.kind IN ('movie','show') AND i.library_id IN (SELECT value FROM json_each(?))
		GROUP BY g.value ORDER BY COUNT(*) DESC, g.value LIMIT ?`, string(ids), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

type collectionName struct {
	id   int64
	name string
}

func (s *Store) collectionNames(libraryID int64) ([]collectionName, error) {
	rows, err := s.db.Query(`SELECT id, name FROM collections c WHERE library_id = ? ORDER BY `+collectionOrder, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []collectionName
	for rows.Next() {
		var c collectionName
		if err := rows.Scan(&c.id, &c.name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
