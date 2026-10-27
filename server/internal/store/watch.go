package store

import "strconv"

// WatchedFraction of the runtime after which an item counts as watched: the
// admin's "count as watched after" setting, 90% unless changed.
func (s *Store) WatchedFraction() float64 {
	n, err := strconv.Atoi(s.Setting(SettingWatchedPercent))
	if err != nil || n < 50 || n > 100 {
		n = 90
	}
	return float64(n) / 100
}

// SaveProgress records a playback position. Crossing WatchedFraction marks
// the item watched and resets the position so it leaves Continue Watching.
func (s *Store) SaveProgress(uid, itemID int64, position, duration float64) (bool, error) {
	completed := duration > 0 && position/duration >= s.WatchedFraction()
	if completed {
		position = 0
	}
	_, err := s.db.Exec(`INSERT INTO watch_state (user_id, item_id, position, duration, watched, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET position = excluded.position, duration = excluded.duration,
			watched = MAX(watch_state.watched, excluded.watched), updated_at = excluded.updated_at`,
		uid, itemID, position, duration, boolInt(completed), now())
	if err != nil || !completed {
		return completed, err
	}
	// Finishing a multi-episode file finishes every episode in it.
	shared, err := s.SharedFileEpisodes(itemID)
	if err != nil || len(shared) == 0 {
		return completed, err
	}
	return completed, s.SetWatched(uid, shared, true)
}

func (s *Store) SetWatched(uid int64, itemIDs []int64, watched bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := now()
	for _, id := range itemIDs {
		if _, err := tx.Exec(`INSERT INTO watch_state (user_id, item_id, position, duration, watched, updated_at)
			VALUES (?, ?, 0, 0, ?, ?)
			ON CONFLICT (user_id, item_id) DO UPDATE SET position = 0, watched = excluded.watched, updated_at = excluded.updated_at`,
			uid, id, boolInt(watched), t); err != nil {
			return err
		}
	}
	return tx.Commit()
}
