package store

// Kid mode for Live TV: a user marked as a kid only sees the channels an
// admin approved for kids, either one at a time or by the channel's group.

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
