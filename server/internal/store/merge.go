package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ItemAlias returns the movie or show a folder's match key was merged into,
// with that item's own match key, or ok false when the key isn't an alias.
func (s *Store) ItemAlias(libraryID int64, matchKey string) (itemID int64, itemKey string, ok bool) {
	err := s.db.QueryRow(`SELECT a.item_id, i.match_key FROM item_aliases a JOIN items i ON i.id = a.item_id
		WHERE a.library_id = ? AND a.match_key = ?`, libraryID, matchKey).Scan(&itemID, &itemKey)
	return itemID, itemKey, err == nil
}

// Merged is one movie or show folded into another by MergeDuplicates.
type Merged struct {
	Kind, Title string
	ID, KeptID  int64 // the duplicate (now gone) and the item it joined
}

// Tables whose rows follow a merged item to the one kept. A row the kept
// item already has (the same user's watch state, say) stays with the
// duplicate and goes when it is deleted.
var mergeTables = []string{"watch_state", "watchlist", "collection_items", "playlist_items", "markers",
	"plays", "playout_items", "external_ratings", "recommendations"}

type mergeCandidate struct {
	id, tmdb, tvdb   int64
	kind, key, title string
	imdb, edition    string
	size             int // episodes or files: the biggest is kept
}

// ids are what a candidate is known by, one per source.
func (c *mergeCandidate) ids() map[string]string {
	m := map[string]string{}
	if c.tmdb > 0 {
		m["tmdb"] = fmt.Sprint(c.tmdb)
	}
	if c.tvdb > 0 {
		m["tvdb"] = fmt.Sprint(c.tvdb)
	}
	if c.imdb != "" {
		m["imdb"] = c.imdb
	}
	return m
}

// MergeDuplicates folds movies and shows of a library that are the same
// title into one, as Plex does. Two titles are the same when they share a
// TMDB, TheTVDB or IMDb id and none of their ids disagree (so a show TMDB
// splits in two that TheTVDB lists as one stays two). Movies must also be the
// same edition. Unmatched titles have no ids and are never merged.
//
// A show's seasons and episodes come together (two copies of an episode
// become two versions of it), a movie's files become versions of the kept
// movie, and watch state, watchlists, collections and playlists follow. The
// title with the most episodes or files is kept.
func (s *Store) MergeDuplicates(libraryID int64) ([]Merged, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT i.id, i.kind, i.match_key, i.title, i.tmdb_id, i.tvdb_id, i.imdb_id, i.edition,
			CASE i.kind WHEN 'show' THEN (SELECT COUNT(*) FROM items e WHERE e.show_id = i.id AND e.kind = 'episode')
				ELSE (SELECT COUNT(*) FROM media_files f WHERE f.item_id = i.id) END
		FROM items i WHERE i.library_id = ? AND i.kind IN ('movie','show') AND i.metadata_source != 'none'
			AND (i.tmdb_id > 0 OR i.tvdb_id > 0 OR i.imdb_id != '')
		ORDER BY i.id`, libraryID)
	if err != nil {
		return nil, err
	}
	var cands []*mergeCandidate
	for rows.Next() {
		c := &mergeCandidate{}
		if err := rows.Scan(&c.id, &c.kind, &c.key, &c.title, &c.tmdb, &c.tvdb, &c.imdb, &c.edition, &c.size); err != nil {
			rows.Close()
			return nil, err
		}
		if c.kind == "movie" {
			c.tvdb = 0 // only shows are matched on TheTVDB
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
	for i, c := range cands {
		parent[i], known[i] = i, c.ids()
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
		for _, src := range []string{"tmdb", "tvdb", "imdb"} {
			v, ok := ids[src]
			if !ok {
				continue
			}
			k := c.kind + "|" + strings.ToLower(c.edition) + "|" + src + ":" + v
			if c.kind == "show" {
				k = "show|" + src + ":" + v
			}
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
			if !agree {
				continue
			}
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
			var err error
			if keep.kind == "show" {
				err = mergeShow(tx, libraryID, keep.id, keep.key, dup.id, dup.key)
			} else {
				err = mergeMovie(tx, libraryID, keep.id, dup.id, dup.key)
			}
			if err != nil {
				return nil, fmt.Errorf("merge %q: %w", dup.title, err)
			}
			merged = append(merged, Merged{Kind: keep.kind, Title: dup.title, ID: dup.id, KeptID: keep.id})
		}
		// The kept title learns the ids its duplicates were known by.
		ids := known[r]
		tvdb, _ := strconv.ParseInt(ids["tvdb"], 10, 64)
		if _, err := tx.Exec(`UPDATE items SET tvdb_id = CASE WHEN tvdb_id = 0 AND kind = 'show' THEN ? ELSE tvdb_id END,
			imdb_id = CASE WHEN imdb_id = '' THEN ? ELSE imdb_id END WHERE id = ?`, tvdb, ids["imdb"], keep.id); err != nil {
			return nil, err
		}
	}
	return merged, tx.Commit()
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

func mergeShow(tx *sql.Tx, libraryID, keepID int64, keepKey string, dupID int64, dupKey string) error {
	if err := aliasTo(tx, libraryID, keepID, dupID, dupKey); err != nil {
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
		if _, err := tx.Exec(`UPDATE OR IGNORE media_file_episodes SET item_id = ? WHERE item_id = ?`, epID, e.id); err != nil {
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
