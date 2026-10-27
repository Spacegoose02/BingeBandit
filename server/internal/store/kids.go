package store

import "database/sql"

// Kid mode: a user marked as a kid only sees the Live TV channels an admin
// approved for kids, either one at a time or by the channel's group. Parents
// can also approve single movies and shows for a kid from libraries the kid
// can't browse, one at a time (kid_titles) or a whole collection at once
// (kid_collections); visible in items.go honours both.

// kidChannels is a condition on channels c for the user bound to its one
// parameter: anyone who isn't a kid gets every channel (so does uid 0, used
// for the server's own lookups).
const kidChannels = `(NOT EXISTS (SELECT 1 FROM users ku WHERE ku.id = ? AND ku.kid = 1 AND ku.is_admin = 0)
	OR c.kids = 1 OR c.grp IN (SELECT grp FROM kid_channel_groups))`

// KidGroup is a channel group and whether all of it is approved for kids.
type KidGroup struct {
	Name     string `json:"name"`
	Channels int    `json:"channels"`
	Approved bool   `json:"approved"`
}

// KidApprovals lists the channel groups (with approval) and the channels
// approved one by one.
func (s *Store) KidApprovals() ([]KidGroup, []int64, error) {
	rows, err := s.db.Query(`SELECT c.grp, COUNT(*), c.grp IN (SELECT grp FROM kid_channel_groups)
		FROM channels c WHERE c.grp != '' GROUP BY c.grp ORDER BY c.grp COLLATE NOCASE`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	groups := []KidGroup{}
	for rows.Next() {
		var g KidGroup
		if err := rows.Scan(&g.Name, &g.Channels, &g.Approved); err != nil {
			return nil, nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()
	rows, err = s.db.Query(`SELECT id FROM channels WHERE kids = 1 ORDER BY number`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
	}
	return groups, ids, rows.Err()
}

// SetKidChannels approves channels for kids, or takes the approval away.
func (s *Store) SetKidChannels(ids []int64, on bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE channels SET kids = ? WHERE id = ?`, boolInt(on), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetKidGroup approves every channel in a group for kids, including ones
// added to it later, or takes that approval away.
func (s *Store) SetKidGroup(name string, on bool) error {
	q := `DELETE FROM kid_channel_groups WHERE grp = ?`
	if on {
		q = `INSERT OR IGNORE INTO kid_channel_groups (grp) VALUES (?)`
	}
	_, err := s.db.Exec(q, name)
	return err
}

// KidTitles lists the movies or shows (kind) approved for a kid, by title.
func (s *Store) KidTitles(uid int64, kind string) ([]*Item, error) {
	return s.queryItems(listSelect+` WHERE i.kind = @kind AND i.id IN (SELECT item_id FROM kid_titles WHERE user_id = @uid)
		ORDER BY i.sort_title COLLATE NOCASE, i.id`, uidArg(uid), sql.Named("kind", kind))
}

// KidTitleUsers lists the users a title is approved for.
func (s *Store) KidTitleUsers(itemID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT user_id FROM kid_titles WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SetKidTitle approves a movie or show for a kid (by is who approved it),
// or takes the approval away.
func (s *Store) SetKidTitle(userID, itemID, by int64, on bool) error {
	if !on {
		_, err := s.db.Exec(`DELETE FROM kid_titles WHERE user_id = ? AND item_id = ?`, userID, itemID)
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO kid_titles (user_id, item_id, added_by, created_at) VALUES (?, ?, ?, ?)`, userID, itemID, by, now())
	return err
}

// KidCollections lists the collections approved for a kid from libraries of
// movies (kind "movie") or shows (kind "show").
func (s *Store) KidCollections(uid int64, kind string) ([]*Collection, error) {
	return s.queryCollections(collectionSelect+` JOIN kid_collections kc ON kc.collection_id = c.id AND kc.user_id = ?
		JOIN libraries l ON l.id = c.library_id AND l.kind = ?
		ORDER BY `+collectionOrder, uid, kind+"s")
}

// KidCollectionApproved reports whether collection id is approved for uid.
func (s *Store) KidCollectionApproved(uid, id int64) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM kid_collections WHERE user_id = ? AND collection_id = ?`, uid, id).Scan(&n)
	return n > 0
}

// KidCollectionUsers lists the users a collection is approved for.
func (s *Store) KidCollectionUsers(id int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT user_id FROM kid_collections WHERE collection_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out[uid] = true
	}
	return out, rows.Err()
}

// SetKidCollection approves a whole collection for a kid (by is who
// approved it), or takes the approval away. Titles approved one by one keep
// their own approval either way.
func (s *Store) SetKidCollection(userID, id, by int64, on bool) error {
	if !on {
		_, err := s.db.Exec(`DELETE FROM kid_collections WHERE user_id = ? AND collection_id = ?`, userID, id)
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO kid_collections (user_id, collection_id, added_by, created_at) VALUES (?, ?, ?, ?)`, userID, id, by, now())
	return err
}
