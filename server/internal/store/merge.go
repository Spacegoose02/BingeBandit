package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// KeyYear is the year in a movie's match key ("movie:thelionking:2019"),
// taken from its folder or file name, or 0 when the name had none.
func KeyYear(matchKey string) int {
	k := matchKey
	if i := strings.Index(k, "|"); i >= 0 {
		k = k[:i]
	}
	i := strings.LastIndex(k, ":")
	if i < 0 {
		return 0
	}
	y, _ := strconv.Atoi(k[i+1:])
	return y
}

// SameYear says whether two years named on folders can be one movie. A year
// out either way allows for a release that came out around New Year; an
// unknown year matches any.
func SameYear(a, b int) bool {
	return a == 0 || b == 0 || a-b <= 1 && b-a <= 1
}

// DropAlias forgets that a folder's match key was merged into another movie,
// so the next scan gives it its own again.
func (s *Store) DropAlias(libraryID int64, matchKey string) error {
	_, err := s.db.Exec(`DELETE FROM item_aliases WHERE library_id = ? AND match_key = ?`, libraryID, matchKey)
	return err
}

// ItemAlias returns the movie a folder's match key was merged into, with that
// movie's own match key, or ok false when the key isn't an alias.
func (s *Store) ItemAlias(libraryID int64, matchKey string) (itemID int64, itemKey string, ok bool) {
	err := s.db.QueryRow(`SELECT a.item_id, i.match_key FROM item_aliases a JOIN items i ON i.id = a.item_id
		WHERE a.library_id = ? AND a.match_key = ?`, libraryID, matchKey).Scan(&itemID, &itemKey)
	return itemID, itemKey, err == nil
}

// Merged is one movie folded into another by MergeDuplicates.
type Merged struct {
	Kind, Title string
	ID, KeptID  int64 // the duplicate (now gone) and the item it joined
}

// Tables whose rows follow a merged item to the one kept. A row the kept
// item already has (the same user's watch state, say) stays with the
// duplicate and goes when it is deleted.
var mergeTables = []string{"watch_state", "watchlist", "favorites", "collection_items", "playlist_items", "markers",
	"plays", "playout_items", "external_ratings", "recommendations"}

type mergeCandidate struct {
	id, tmdb         int64
	kind, key, title string
	imdb, edition    string
	size             int // files: the movie with the most is kept
}

// ids are what a candidate is known by, one per source.
func (c *mergeCandidate) ids() map[string]string {
	m := map[string]string{}
	if c.tmdb > 0 {
		m["tmdb"] = fmt.Sprint(c.tmdb)
	}
	if c.imdb != "" {
		m["imdb"] = c.imdb
	}
	return m
}

// MergeDuplicates folds movies of a library that are the same title into
// one, as Plex does. Two movies are the same when they share a TMDB or IMDb
// id, none of their ids disagree, they are the same edition and their folders
// don't name years more than one apart: The Lion King (1994) and (2019)
// stay two movies even when one was matched to the other wrongly, so either
// can be fixed with Match. Unmatched movies have no ids and are never merged. The other copy's files become
// versions of the kept movie (the one with the most files), and watch state,
// watchlists, collections and playlists follow.
//
// Shows are never merged: each show folder is its own show, so two folders
// with the same TMDB id (one matched wrongly, say) stay apart and either can
// be fixed with Match.
func (s *Store) MergeDuplicates(libraryID int64) ([]Merged, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT i.id, i.kind, i.match_key, i.title, i.tmdb_id, i.imdb_id, i.edition,
			(SELECT COUNT(*) FROM media_files f WHERE f.item_id = i.id)
		FROM items i WHERE i.library_id = ? AND i.kind = 'movie' AND i.metadata_source != 'none'
			AND (i.tmdb_id > 0 OR i.imdb_id != '')
		ORDER BY i.id`, libraryID)
	if err != nil {
		return nil, err
	}
	var cands []*mergeCandidate
	for rows.Next() {
		c := &mergeCandidate{}
		if err := rows.Scan(&c.id, &c.kind, &c.key, &c.title, &c.tmdb, &c.imdb, &c.edition, &c.size); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Union-find over candidates, joining two groups only when their ids
	// agree for every source both know.
	parent := make([]int, len(cands))
	known := make([]map[string]string, len(cands))
	years := make([][2]int, len(cands)) // the lowest and highest folder year a group has, 0 for none
	for i, c := range cands {
		parent[i], known[i] = i, c.ids()
		y := KeyYear(c.key)
		years[i] = [2]int{y, y}
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	byID := map[string]int{}
	for i, c := range cands {
		ids := c.ids()
		for _, src := range []string{"tmdb", "imdb"} {
			v, ok := ids[src]
			if !ok {
				continue
			}
			k := strings.ToLower(c.edition) + "|" + src + ":" + v
			j, seen := byID[k]
			if !seen {
				byID[k] = i
				continue
			}
			a, b := find(i), find(j)
			if a == b {
				continue
			}
			agree := true
			for src, v := range known[a] {
				if w, ok := known[b][src]; ok && w != v {
					agree = false
				}
			}
			ya, yb := years[a], years[b]
			lo, hi := minYear(ya[0], yb[0]), max(ya[1], yb[1])
			if !agree || !SameYear(lo, hi) {
				continue
			}
			years[b] = [2]int{lo, hi}
			parent[a] = b
			for src, v := range known[a] {
				known[b][src] = v
			}
		}
	}
	groups := map[int][]*mergeCandidate{}
	var order []int
	for i, c := range cands {
		r := find(i)
		if _, ok := groups[r]; !ok {
			order = append(order, r)
		}
		groups[r] = append(groups[r], c)
	}

	var merged []Merged
	for _, r := range order {
		g := groups[r]
		if len(g) < 2 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool { return g[i].size > g[j].size })
		keep := g[0]
		for _, dup := range g[1:] {
			if err := mergeMovie(tx, libraryID, keep.id, dup.id, dup.key); err != nil {
				return nil, fmt.Errorf("merge %q: %w", dup.title, err)
			}
			merged = append(merged, Merged{Kind: keep.kind, Title: dup.title, ID: dup.id, KeptID: keep.id})
		}
		// The kept movie learns the IMDb id its duplicates were known by.
		if _, err := tx.Exec(`UPDATE items SET imdb_id = CASE WHEN imdb_id = '' THEN ? ELSE imdb_id END WHERE id = ?`, known[r]["imdb"], keep.id); err != nil {
			return nil, err
		}
	}
	return merged, tx.Commit()
}

// minYear is the lower of two years, ignoring an unknown (0) one.
func minYear(a, b int) int {
	if a == 0 || b != 0 && b < a {
		return b
	}
	return a
}

// aliasTo makes a merged folder's match key (and keys already merged into
// the duplicate) point at the kept item, so later scans add to it.
func aliasTo(tx *sql.Tx, libraryID, keepID, dupID int64, dupKey string) error {
	if _, err := tx.Exec(`UPDATE item_aliases SET item_id = ? WHERE item_id = ?`, keepID, dupID); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO item_aliases (library_id, match_key, item_id) VALUES (?, ?, ?)
		ON CONFLICT (library_id, match_key) DO UPDATE SET item_id = excluded.item_id`, libraryID, dupKey, keepID)
	return err
}

func mergeMovie(tx *sql.Tx, libraryID, keepID, dupID int64, dupKey string) error {
	if err := aliasTo(tx, libraryID, keepID, dupID, dupKey); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE media_files SET item_id = ? WHERE item_id = ?`, keepID, dupID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE OR IGNORE media_file_episodes SET item_id = ? WHERE item_id = ?`, keepID, dupID); err != nil {
		return err
	}
	if err := moveRows(tx, dupID, keepID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM items WHERE id = ?`, dupID)
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
