package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Custom channels are ordinary channels (custom = 1) whose guide id is
// "custom.<channel id>" and whose content comes from the library.

func CustomEPGID(channelID int64) string { return fmt.Sprintf("custom.%d", channelID) }

func CustomChannelID(epgID string) (int64, bool) {
	if !strings.HasPrefix(epgID, "custom.") {
		return 0, false
	}
	id, err := strconv.ParseInt(epgID[len("custom."):], 10, 64)
	return id, err == nil
}

// ContentSource is one thing a block draws from.
type ContentSource struct {
	Type  string `json:"type"` // library | show | collection | playlist | label
	ID    int64  `json:"id,omitempty"`
	Label string `json:"label,omitempty"` // the label, for type label
	Name  string `json:"name,omitempty"`  // display name, saved so editors can show it
}

// Block is a time-of-day slot of a custom channel. StartMinute -1 marks the
// all-day default block used whenever no timed block applies.
type Block struct {
	ID          int64           `json:"id"`
	StartMinute int             `json:"startMinute"`
	EndMinute   int             `json:"endMinute"`
	Mode        string          `json:"mode"` // shuffle | order
	Sources     []ContentSource `json:"sources"`
	Cursor      int             `json:"-"`
}

func (s *Store) CustomBlocks(channelID int64) ([]*Block, error) {
	rows, err := s.db.Query(`SELECT id, start_minute, end_minute, mode, sources, cursor FROM custom_blocks
		WHERE channel_id = ? ORDER BY position`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Block{}
	for rows.Next() {
		var b Block
		var src string
		if err := rows.Scan(&b.ID, &b.StartMinute, &b.EndMinute, &b.Mode, &src, &b.Cursor); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(src), &b.Sources)
		out = append(out, &b)
	}
	return out, rows.Err()
}

// SaveCustomChannel creates or updates a custom channel and replaces its blocks.
func (s *Store) SaveCustomChannel(c *Channel, blocks []*Block) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if c.ID == 0 {
		res, err := tx.Exec(`INSERT INTO channels (number, name, logo, grp, epg_id, enabled, custom, created_at) VALUES (?, ?, ?, ?, '', ?, 1, ?)`,
			c.Number, c.Name, c.Logo, c.Group, boolInt(c.Enabled), now())
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
	} else if _, err := tx.Exec(`UPDATE channels SET number = ?, name = ?, logo = ?, grp = ?, enabled = ? WHERE id = ? AND custom = 1`,
		c.Number, c.Name, c.Logo, c.Group, boolInt(c.Enabled), c.ID); err != nil {
		return err
	}
	c.EPGID, c.Custom = CustomEPGID(c.ID), true
	if _, err := tx.Exec(`UPDATE channels SET epg_id = ? WHERE id = ?`, c.EPGID, c.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM custom_blocks WHERE channel_id = ?`, c.ID); err != nil {
		return err
	}
	for i, b := range blocks {
		src, _ := json.Marshal(b.Sources)
		if _, err := tx.Exec(`INSERT INTO custom_blocks (channel_id, position, start_minute, end_minute, mode, sources) VALUES (?, ?, ?, ?, ?, ?)`,
			c.ID, i, b.StartMinute, b.EndMinute, b.Mode, string(src)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetBlockCursor(blockID int64, cursor int) error {
	_, err := s.db.Exec(`UPDATE custom_blocks SET cursor = ? WHERE id = ?`, cursor, blockID)
	return err
}

// Playable is one movie or episode a custom channel can air.
type Playable struct {
	ItemID      int64
	FileID      int64
	Duration    float64
	Title       string
	Subtitle    string
	Description string
	Category    string
	Icon        string
}

const playableSelect = `SELECT i.id, f.id, f.duration,
	CASE WHEN i.kind = 'episode' THEN COALESCE(sh.title, i.title) ELSE i.title END,
	CASE WHEN i.kind = 'episode' THEN 'S' || i.season_number || ' · E' || i.episode_number || ' ' || i.title ELSE '' END,
	i.overview, COALESCE(json_extract(COALESCE(sh.genres, i.genres), '$[0]'), ''), COALESCE(NULLIF(sh.poster, ''), i.poster)
FROM items i
JOIN media_files f ON f.id = (SELECT f2.id FROM media_files f2 WHERE f2.item_id = i.id AND f2.duration > 0 ORDER BY f2.height DESC LIMIT 1)
LEFT JOIN items sh ON sh.id = i.show_id
WHERE i.kind IN ('movie','episode') AND `

// ResolveSource lists the playable items behind a source, in natural order
// (episodes by season/episode, movies by release date).
func (s *Store) ResolveSource(src ContentSource) ([]Playable, error) {
	order := ` ORDER BY i.show_id, COALESCE(i.season_number, 0) = 0, i.season_number, i.episode_number, i.release_date, i.sort_title`
	var q string
	var args []any
	switch src.Type {
	case "library":
		q, args = `i.library_id = ?`, []any{src.ID}
	case "show":
		q, args = `i.show_id = ?`, []any{src.ID}
	case "collection":
		q, args = `(i.id IN (SELECT ci.item_id FROM collection_items ci WHERE ci.collection_id = ?1)
			OR i.show_id IN (SELECT ci.item_id FROM collection_items ci WHERE ci.collection_id = ?1)
			OR EXISTS (SELECT 1 FROM collections c, items top, json_each(top.labels) j
				WHERE c.id = ?1 AND c.label_rule != '' AND top.id = COALESCE(i.show_id, i.id) AND top.library_id = c.library_id
				AND j.value = c.label_rule COLLATE NOCASE))`, []any{src.ID}
	case "playlist":
		q, args = `i.id IN (SELECT item_id FROM playlist_items WHERE playlist_id = ?)`, []any{src.ID}
		order = ` ORDER BY (SELECT position FROM playlist_items WHERE playlist_id = ` + strconv.FormatInt(src.ID, 10) + ` AND item_id = i.id)`
	case "label":
		q, args = `EXISTS (SELECT 1 FROM items top, json_each(top.labels) j WHERE top.id = COALESCE(i.show_id, i.id) AND j.value = ? COLLATE NOCASE)`, []any{src.Label}
	default:
		return nil, fmt.Errorf("unknown source type %q", src.Type)
	}
	rows, err := s.db.Query(playableSelect+q+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playable{}
	for rows.Next() {
		var p Playable
		if err := rows.Scan(&p.ItemID, &p.FileID, &p.Duration, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Icon); err != nil {
			return nil, err
		}
		p.Icon = ImageURL(p.Icon)
		out = append(out, p)
	}
	return out, rows.Err()
}

type PlayoutItem struct {
	ID          int64  `json:"id"`
	ChannelID   int64  `json:"channelId"`
	ItemID      int64  `json:"itemId,omitempty"`
	FileID      int64  `json:"fileId"`
	Start       int64  `json:"start"`
	Stop        int64  `json:"stop"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

func (s *Store) AddPlayout(items []PlayoutItem) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO playout_items (channel_id, item_id, file_id, start_at, stop_at, title, subtitle, description, category, icon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	for _, it := range items {
		if _, err := stmt.Exec(it.ChannelID, it.ItemID, it.FileID, it.Start, it.Stop, it.Title, it.Subtitle, it.Description, it.Category, it.Icon); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PlayoutEnd is when the channel's built schedule runs out (0 if empty).
func (s *Store) PlayoutEnd(channelID int64) (int64, error) {
	var end int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(stop_at), 0) FROM playout_items WHERE channel_id = ?`, channelID).Scan(&end)
	return end, err
}

// ClearPlayoutAfter drops schedule entries that start after t (used after
// edits; whatever is airing now keeps playing).
func (s *Store) ClearPlayoutAfter(channelID, t int64) error {
	_, err := s.db.Exec(`DELETE FROM playout_items WHERE channel_id = ? AND start_at > ?`, channelID, t)
	return err
}

// CancelPlayout takes one entry off a custom channel's schedule and moves
// everything after it up to fill the gap. An entry that is airing ends now.
// Reports whether it was airing, so the caller can restart the stream.
func (s *Store) CancelPlayout(channelID, entryID, now int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var start, stop int64
	err = tx.QueryRow(`SELECT start_at, stop_at FROM playout_items WHERE id = ? AND channel_id = ?`, entryID, channelID).Scan(&start, &stop)
	if errors.Is(err, sql.ErrNoRows) || err == nil && stop <= now {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	airing := start <= now
	shift := stop - max(start, now)
	if start < now {
		_, err = tx.Exec(`UPDATE playout_items SET stop_at = ? WHERE id = ?`, now, entryID)
	} else {
		_, err = tx.Exec(`DELETE FROM playout_items WHERE id = ?`, entryID)
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE playout_items SET start_at = start_at - ?, stop_at = stop_at - ? WHERE channel_id = ? AND start_at >= ?`,
		shift, shift, channelID, stop); err != nil {
		return false, err
	}
	return airing, tx.Commit()
}

// EndPlayoutAt ends whatever airs at t and drops everything after it, so the
// schedule can be built again from t.
func (s *Store) EndPlayoutAt(channelID, t int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM playout_items WHERE channel_id = ? AND start_at >= ?`, channelID, t); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE playout_items SET stop_at = ? WHERE channel_id = ? AND stop_at > ?`, t, channelID, t); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PrunePlayout(before int64) error {
	_, err := s.db.Exec(`DELETE FROM playout_items WHERE stop_at < ?`, before)
	return err
}

// PlayoutAt returns what airs on a channel at time t.
func (s *Store) PlayoutAt(channelID, t int64) (*PlayoutItem, error) {
	var p PlayoutItem
	var item sql.NullInt64
	err := s.db.QueryRow(`SELECT id, channel_id, item_id, file_id, start_at, stop_at, title, subtitle, description, category, icon
		FROM playout_items WHERE channel_id = ? AND start_at <= ? AND stop_at > ? ORDER BY start_at DESC LIMIT 1`, channelID, t, t).
		Scan(&p.ID, &p.ChannelID, &item, &p.FileID, &p.Start, &p.Stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Icon)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	p.ItemID = item.Int64
	return &p, err
}

// PlayoutPrograms returns a custom channel's schedule as guide entries.
// Their ids are negative so they never collide with XMLTV programs.
func (s *Store) PlayoutPrograms(channelID, from, to int64) ([]GuideProgram, error) {
	rows, err := s.db.Query(`SELECT id, start_at, stop_at, title, subtitle, description, category, icon FROM playout_items
		WHERE channel_id = ? AND start_at < ? AND stop_at > ? ORDER BY start_at`, channelID, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GuideProgram{}
	for rows.Next() {
		p := GuideProgram{EPGID: CustomEPGID(channelID)}
		if err := rows.Scan(&p.ID, &p.Start, &p.Stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Icon); err != nil {
			return nil, err
		}
		p.ID = -p.ID
		out = append(out, p)
	}
	return out, rows.Err()
}

// CustomChannelIDs lists every custom channel, for the playout refill task.
func (s *Store) CustomChannelIDs() ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM channels WHERE custom = 1`)
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
