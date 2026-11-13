package store

// Marker is a skippable section of an episode or movie, in seconds.
type Marker struct {
	Kind   string  `json:"kind"` // intro | credits | recap | commercial (several to a recording)
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
		ON CONFLICT (item_id, kind) WHERE kind != 'commercial' DO UPDATE SET start_at = excluded.start_at, end_at = excluded.end_at
		WHERE markers.source != 'manual'`, itemID, kind, start, end)
	return err
}

// DeleteDetectedMarker removes a detected marker that detection no longer
// finds. One an admin set by hand stays.
func (s *Store) DeleteDetectedMarker(itemID int64, kind string) error {
	_, err := s.db.Exec(`DELETE FROM markers WHERE item_id = ? AND kind = ? AND source = 'detected'`, itemID, kind)
	return err
}

// SetDetectedCommercials replaces a recording's detected ad breaks. Breaks an
// admin set by hand are kept, and while there are any, detection leaves the
// file alone.
func (s *Store) SetDetectedCommercials(itemID int64, breaks [][2]float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var manual int
	tx.QueryRow(`SELECT COUNT(*) FROM markers WHERE item_id = ? AND kind = 'commercial' AND source = 'manual'`, itemID).Scan(&manual)
	if manual > 0 {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM markers WHERE item_id = ? AND kind = 'commercial'`, itemID); err != nil {
		return err
	}
	for _, b := range breaks {
		if _, err := tx.Exec(`INSERT INTO markers (item_id, kind, start_at, end_at, source) VALUES (?, 'commercial', ?, ?, 'detected')`,
			itemID, b[0], b[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetManualMarkers replaces an item's markers with admin-entered ones. Kinds
// left out are cleared (ad breaks only when the list has some) and the item
// is queued for detection again.
func (s *Store) SetManualMarkers(itemID int64, ms []Marker) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Ad breaks are kept unless the edit lists its own (the intro and credits
	// editor doesn't show them).
	keepAds := true
	for _, m := range ms {
		keepAds = keepAds && m.Kind != "commercial"
	}
	if _, err := tx.Exec(`DELETE FROM markers WHERE item_id = ? AND (? = 0 OR kind != 'commercial')`, itemID, boolInt(keepAds)); err != nil {
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

// MoviesNeedingMarkers lists movies whose end credits haven't been looked for
// yet, in libraries where detection is on.
func (s *Store) MoviesNeedingMarkers() ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM items WHERE kind = 'movie' AND markers_at = 0
		AND library_id IN (SELECT id FROM libraries WHERE skip_markers = 0) ORDER BY id`)
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

// RecordingToScan is a finished DVR recording, as the library knows it,
// whose ad breaks haven't been looked for.
type RecordingToScan struct {
	RecordingID int64
	ItemID      int64
	Path        string
	Duration    float64
}

// RecordingsNeedingCommercials lists finished recordings that a library has
// picked up and whose ad breaks haven't been looked for, oldest first.
func (s *Store) RecordingsNeedingCommercials(limit int) ([]RecordingToScan, error) {
	rows, err := s.db.Query(`SELECT r.id, f.item_id, f.path, f.duration FROM recordings r
		JOIN media_files f ON f.path = r.path
		WHERE r.status = 'completed' AND r.commercials_at = 0 AND r.path != ''
		ORDER BY r.stop_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordingToScan
	for rows.Next() {
		var r RecordingToScan
		if err := rows.Scan(&r.RecordingID, &r.ItemID, &r.Path, &r.Duration); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkCommercialsChecked records that a recording's ad breaks were looked for.
func (s *Store) MarkCommercialsChecked(recordingID int64) error {
	_, err := s.db.Exec(`UPDATE recordings SET commercials_at = ? WHERE id = ?`, now(), recordingID)
	return err
}
