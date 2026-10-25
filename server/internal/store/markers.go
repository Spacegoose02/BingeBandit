package store

// Marker is a skippable section of an episode or movie, in seconds.
type Marker struct {
	Kind   string  `json:"kind"` // intro | credits
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Source string  `json:"source"` // detected | manual
}

func (s *Store) Markers(itemID int64) ([]Marker, error) {
	rows, err := s.db.Query(`SELECT kind, start_at, end_at, source FROM markers WHERE item_id = ? ORDER BY start_at`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Marker{}
	for rows.Next() {
		var m Marker
		if err := rows.Scan(&m.Kind, &m.Start, &m.End, &m.Source); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetDetectedMarker stores a detection result unless an admin set that marker by hand.
func (s *Store) SetDetectedMarker(itemID int64, kind string, start, end float64) error {
	_, err := s.db.Exec(`INSERT INTO markers (item_id, kind, start_at, end_at, source) VALUES (?, ?, ?, ?, 'detected')
		ON CONFLICT (item_id, kind) DO UPDATE SET start_at = excluded.start_at, end_at = excluded.end_at
		WHERE markers.source != 'manual'`, itemID, kind, start, end)
	return err
}

// SetManualMarkers replaces an item's markers with admin-entered ones. Kinds
// left out are cleared and the item is queued for detection again.
func (s *Store) SetManualMarkers(itemID int64, ms []Marker) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM markers WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, m := range ms {
		if _, err := tx.Exec(`INSERT INTO markers (item_id, kind, start_at, end_at, source) VALUES (?, ?, ?, ?, 'manual')`,
			itemID, m.Kind, m.Start, m.End); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE items SET markers_at = 0 WHERE id = ?`, itemID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkMarkersScanned(itemIDs []int64) error {
	t := now()
	for _, id := range itemIDs {
		if _, err := s.db.Exec(`UPDATE items SET markers_at = ? WHERE id = ?`, t, id); err != nil {
			return err
		}
	}
	return nil
}

// SeasonsNeedingMarkers lists seasons with at least two episodes and some
// episode not yet analyzed, in libraries where detection is on.
func (s *Store) SeasonsNeedingMarkers() ([]int64, error) {
	rows, err := s.db.Query(`SELECT parent_id FROM items WHERE kind = 'episode'
		AND library_id IN (SELECT id FROM libraries WHERE skip_markers = 0) GROUP BY parent_id
		HAVING COUNT(*) >= 2 AND SUM(markers_at = 0) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SeasonIDs returns the seasons of a show, or the season itself.
func (s *Store) SeasonIDs(id int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM items WHERE kind = 'season' AND (id = ?1 OR parent_id = ?1) ORDER BY season_number`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var sid int64
		if err := rows.Scan(&sid); err != nil {
			return nil, err
		}
		out = append(out, sid)
	}
	return out, rows.Err()
}
