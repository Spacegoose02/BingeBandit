package store

import (
	"database/sql"
	"errors"
)

func (s *Store) AddToWatchlist(uid, itemID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO watchlist (user_id, item_id, added_at) VALUES (?, ?, ?)`, uid, itemID, now())
	return err
}

func (s *Store) RemoveFromWatchlist(uid, itemID int64) error {
	_, err := s.db.Exec(`DELETE FROM watchlist WHERE user_id = ? AND item_id = ?`, uid, itemID)
	return err
}

// Watchlist lists the user's watchlist, newest additions first.
func (s *Store) Watchlist(uid int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` JOIN watchlist wl2 ON wl2.item_id = i.id AND wl2.user_id = @uid
		WHERE `+visible+` ORDER BY wl2.added_at DESC`, uidArg(uid))
}

type Playlist struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"userId"`
	Name      string  `json:"name"`
	ItemCount int     `json:"itemCount"`
	Duration  float64 `json:"duration"` // seconds
	Poster    string  `json:"poster,omitempty"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
}

const playlistSelect = `SELECT p.id, p.user_id, p.name, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM playlist_items pi WHERE pi.playlist_id = p.id),
	(SELECT COALESCE(SUM((SELECT MAX(f.duration) FROM media_files f WHERE f.item_id = pi.item_id)), 0)
		FROM playlist_items pi WHERE pi.playlist_id = p.id),
	COALESCE((SELECT COALESCE(NULLIF(sh.poster, ''), it.poster) FROM playlist_items pi
		JOIN items it ON it.id = pi.item_id LEFT JOIN items sh ON sh.id = it.show_id
		WHERE pi.playlist_id = p.id ORDER BY pi.position LIMIT 1), '')
FROM playlists p`

func scanPlaylist(r interface{ Scan(...any) error }) (*Playlist, error) {
	var p Playlist
	err := r.Scan(&p.ID, &p.UserID, &p.Name, &p.CreatedAt, &p.UpdatedAt, &p.ItemCount, &p.Duration, &p.Poster)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	p.Poster = ImageURL(p.Poster)
	return &p, err
}

func (s *Store) Playlists(uid int64) ([]*Playlist, error) {
	rows, err := s.db.Query(playlistSelect+` WHERE p.user_id = ? ORDER BY p.updated_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Playlist returns a playlist only if uid owns it.
func (s *Store) Playlist(uid, id int64) (*Playlist, error) {
	return scanPlaylist(s.db.QueryRow(playlistSelect+` WHERE p.id = ? AND p.user_id = ?`, id, uid))
}

func (s *Store) CreatePlaylist(uid int64, name string) (*Playlist, error) {
	t := now()
	res, err := s.db.Exec(`INSERT INTO playlists (user_id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`, uid, name, t, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Playlist(uid, id)
}

func (s *Store) RenamePlaylist(id int64, name string) error {
	_, err := s.db.Exec(`UPDATE playlists SET name = ?, updated_at = ? WHERE id = ?`, name, now(), id)
	return err
}

func (s *Store) DeletePlaylist(id int64) error {
	_, err := s.db.Exec(`DELETE FROM playlists WHERE id = ?`, id)
	return err
}

func (s *Store) PlaylistItems(uid, id int64) ([]*Item, error) {
	return s.queryItems(itemSelect+` JOIN playlist_items pi ON pi.item_id = i.id AND pi.playlist_id = @p
		WHERE `+visible+` ORDER BY pi.position`, uidArg(uid), sql.Named("p", id))
}

// AddToPlaylist appends items, skipping ones already in the playlist.
func (s *Store) AddToPlaylist(id int64, itemIDs []int64) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var pos int
	tx.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM playlist_items WHERE playlist_id = ?`, id).Scan(&pos)
	added := 0
	for _, item := range itemIDs {
		res, err := tx.Exec(`INSERT OR IGNORE INTO playlist_items (playlist_id, item_id, position) VALUES (?, ?, ?)`, id, item, pos)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
			pos++
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, now(), id); err != nil {
		return 0, err
	}
	return added, tx.Commit()
}

func (s *Store) RemoveFromPlaylist(id, itemID int64) error {
	_, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id = ? AND item_id = ?`, id, itemID)
	return err
}

// ReorderPlaylist sets positions from the given order; items not listed keep
// their relative order after the listed ones.
func (s *Store) ReorderPlaylist(id int64, itemIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE playlist_items SET position = position + 1000000 WHERE playlist_id = ?`, id); err != nil {
		return err
	}
	for i, item := range itemIDs {
		if _, err := tx.Exec(`UPDATE playlist_items SET position = ? WHERE playlist_id = ? AND item_id = ?`, i, id, item); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// PlaylistNext is the item after itemID in the playlist, for continuous play.
func (s *Store) PlaylistNext(uid, id, itemID int64) (*Item, error) {
	return scanItem(s.db.QueryRow(itemSelect+` JOIN playlist_items pi ON pi.item_id = i.id AND pi.playlist_id = @p
		WHERE `+visible+` AND pi.position > (SELECT position FROM playlist_items WHERE playlist_id = @p AND item_id = @cur)
		ORDER BY pi.position LIMIT 1`, uidArg(uid), sql.Named("p", id), sql.Named("cur", itemID)))
}
