package store

import (
	"database/sql"
	"errors"
)

// DiscoverSource is a YouTube playlist or channel shown on the Discover page.
type DiscoverSource struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // playlist | channel
	YouTubeID string `json:"youtubeId"`
	Position  int    `json:"position"`
	FetchedAt int64  `json:"fetchedAt"`
	Error     string `json:"error,omitempty"` // why the last fetch failed
}

// DiscoverVideo is one video of a source.
type DiscoverVideo struct {
	ID          string `json:"id"` // YouTube video id
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	PublishedAt int64  `json:"publishedAt"`
}

// discoverKeep is how many videos each source keeps; older ones are dropped.
const discoverKeep = 50

func (s *Store) DiscoverSources() ([]*DiscoverSource, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, youtube_id, position, fetched_at, error FROM discover_sources ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DiscoverSource{}
	for rows.Next() {
		var d DiscoverSource
		if err := rows.Scan(&d.ID, &d.Name, &d.Kind, &d.YouTubeID, &d.Position, &d.FetchedAt, &d.Error); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (s *Store) DiscoverSource(id int64) (*DiscoverSource, error) {
	var d DiscoverSource
	err := s.db.QueryRow(`SELECT id, name, kind, youtube_id, position, fetched_at, error FROM discover_sources WHERE id = ?`, id).
		Scan(&d.ID, &d.Name, &d.Kind, &d.YouTubeID, &d.Position, &d.FetchedAt, &d.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &d, err
}

// AddDiscoverSource adds a source at the end of the list.
func (s *Store) AddDiscoverSource(d *DiscoverSource) error {
	res, err := s.db.Exec(`INSERT INTO discover_sources (name, kind, youtube_id, position, created_at)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1 FROM discover_sources), ?)`, d.Name, d.Kind, d.YouTubeID, now())
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) RenameDiscoverSource(id int64, name string) error {
	return s.execOne(`UPDATE discover_sources SET name = ? WHERE id = ?`, name, id)
}

func (s *Store) DeleteDiscoverSource(id int64) error {
	return s.execOne(`DELETE FROM discover_sources WHERE id = ?`, id)
}

// OrderDiscoverSources sets the display order to the given ids.
func (s *Store) OrderDiscoverSources(ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE discover_sources SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveDiscoverVideos records a fetch: the videos found (kept up to
// discoverKeep, newest first) or the error.
func (s *Store) SaveDiscoverVideos(id int64, videos []DiscoverVideo, fetchErr string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, v := range videos {
		if _, err := tx.Exec(`INSERT INTO discover_videos (source_id, video_id, title, description, published_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (source_id, video_id) DO UPDATE SET title = excluded.title, description = excluded.description`,
			id, v.ID, v.Title, v.Description, v.PublishedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM discover_videos WHERE source_id = ?1 AND video_id NOT IN
		(SELECT video_id FROM discover_videos WHERE source_id = ?1 ORDER BY published_at DESC LIMIT ?2)`, id, discoverKeep); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE discover_sources SET fetched_at = ?, error = ? WHERE id = ?`, now(), fetchErr, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DiscoverVideos returns a source's newest videos.
func (s *Store) DiscoverVideos(id int64, limit int) ([]DiscoverVideo, error) {
	rows, err := s.db.Query(`SELECT video_id, title, description, published_at FROM discover_videos WHERE source_id = ?
		ORDER BY published_at DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscoverVideo{}
	for rows.Next() {
		var v DiscoverVideo
		if err := rows.Scan(&v.ID, &v.Title, &v.Description, &v.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// HasDiscoverVideo says whether a YouTube video is on the Discover page.
func (s *Store) HasDiscoverVideo(videoID string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM discover_videos WHERE video_id = ?`, videoID).Scan(&n)
	return n > 0, err
}
