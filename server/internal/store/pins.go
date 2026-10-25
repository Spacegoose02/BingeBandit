package store

import "sort"

// pinned restricts items to libraries the @uid user has pinned. A library the
// user never chose about counts as pinned, so new libraries appear on Home.
const pinned = `NOT EXISTS (SELECT 1 FROM library_pins lp WHERE lp.user_id = @uid AND lp.library_id = i.library_id AND lp.pinned = 0)`

// LibraryPin is one library's place in a user's sidebar.
type LibraryPin struct {
	ID     int64 `json:"id"`
	Pinned bool  `json:"pinned"`
}

// PinLibraries sorts libs the way the user ordered them, pinned first, and
// marks which are pinned. Libraries the user never placed keep their order
// after the placed ones and count as pinned.
func (s *Store) PinLibraries(userID int64, libs []*Library) error {
	rows, err := s.db.Query(`SELECT library_id, pinned, position FROM library_pins WHERE user_id = ?`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type place struct {
		pinned bool
		pos    int
	}
	placed := map[int64]place{}
	for rows.Next() {
		var id int64
		var p place
		if err := rows.Scan(&id, &p.pinned, &p.pos); err != nil {
			return err
		}
		placed[id] = p
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rank := func(l *Library) (int, int) {
		p, ok := placed[l.ID]
		switch {
		case !ok:
			return 1, 0 // pinned, after the ones the user ordered
		case p.pinned:
			return 0, p.pos
		default:
			return 2, p.pos
		}
	}
	for _, l := range libs {
		group, _ := rank(l)
		l.Pinned = group < 2
	}
	sort.SliceStable(libs, func(a, b int) bool {
		ga, pa := rank(libs[a])
		gb, pb := rank(libs[b])
		return ga < gb || (ga == gb && pa < pb)
	})
	return nil
}

// PinsChosen reports whether the user has been through the "what to pin" step.
func (s *Store) PinsChosen(userID int64) (bool, error) {
	var chosen bool
	err := s.db.QueryRow(`SELECT pins_chosen FROM users WHERE id = ?`, userID).Scan(&chosen)
	return chosen, err
}

// SetLibraryPins replaces the user's sidebar with pins, in order.
func (s *Store) SetLibraryPins(userID int64, pins []LibraryPin) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM library_pins WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for i, p := range pins {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO library_pins (user_id, library_id, pinned, position)
			SELECT ?, id, ?, ? FROM libraries WHERE id = ?`, userID, p.Pinned, i, p.ID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE users SET pins_chosen = 1 WHERE id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}
