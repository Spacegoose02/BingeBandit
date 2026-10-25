package store

// AcceptsRecommendations reports whether a user opted in to receiving
// recommendations from other people.
func (s *Store) AcceptsRecommendations(userID int64) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM recommendation_optins WHERE user_id = ?`, userID).Scan(&n)
	return n > 0
}

func (s *Store) SetAcceptsRecommendations(userID int64, accept bool) error {
	if accept {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO recommendation_optins (user_id) VALUES (?)`, userID)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM recommendation_optins WHERE user_id = ?`, userID)
	return err
}

// Recipient is someone a recommendation can go to.
type Recipient struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// RecommendationRecipients lists the users other than from who opted in,
// by name. The caller checks each can see the item.
func (s *Store) RecommendationRecipients(from int64) ([]Recipient, error) {
	rows, err := s.db.Query(`SELECT u.id, CASE WHEN u.profile_name != '' THEN u.profile_name ELSE u.username END
		FROM users u JOIN recommendation_optins o ON o.user_id = u.id
		WHERE u.id != ? ORDER BY 2 COLLATE NOCASE`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recipient{}
	for rows.Next() {
		var r Recipient
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Recommend records (or renews) a recommendation. Recommending something
// again brings it back if it was dismissed.
func (s *Store) Recommend(from, to, itemID int64, note string) error {
	_, err := s.db.Exec(`INSERT INTO recommendations (from_user, to_user, item_id, note, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (from_user, to_user, item_id) DO UPDATE SET note = excluded.note, created_at = excluded.created_at, dismissed = 0`,
		from, to, itemID, note, now())
	return err
}

// Recommendation is one title recommended to a user, merged across the
// people who recommended it.
type Recommendation struct {
	ItemID int64    `json:"itemId"`
	From   []string `json:"from"`
	Notes  []string `json:"notes,omitempty"` // "Name: note"
	At     int64    `json:"at"`              // latest
}

// RecommendationsFor lists what was recommended to a user and not
// dismissed, newest first.
func (s *Store) RecommendationsFor(userID int64, limit int) ([]Recommendation, error) {
	rows, err := s.db.Query(`SELECT r.item_id, CASE WHEN u.profile_name != '' THEN u.profile_name ELSE u.username END, r.note, r.created_at
		FROM recommendations r JOIN users u ON u.id = r.from_user
		WHERE r.to_user = ? AND r.dismissed = 0 ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recommendation
	index := map[int64]int{}
	for rows.Next() {
		var id, at int64
		var name, note string
		if err := rows.Scan(&id, &name, &note, &at); err != nil {
			return nil, err
		}
		i, ok := index[id]
		if !ok {
			if len(out) >= limit {
				continue
			}
			i = len(out)
			index[id] = i
			out = append(out, Recommendation{ItemID: id, At: at})
		}
		out[i].From = append(out[i].From, name)
		if note != "" {
			out[i].Notes = append(out[i].Notes, name+": "+note)
		}
	}
	return out, rows.Err()
}

// DismissRecommendation hides a title from a user's recommendations.
func (s *Store) DismissRecommendation(userID, itemID int64) error {
	_, err := s.db.Exec(`UPDATE recommendations SET dismissed = 1 WHERE to_user = ? AND item_id = ?`, userID, itemID)
	return err
}
