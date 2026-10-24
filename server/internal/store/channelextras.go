package store

import (
	"database/sql"
	"errors"
	"strings"
)

// LibraryLogoPrefix marks a channel logo that comes from the logo library
// ("library:<file>") rather than a provider URL.
const LibraryLogoPrefix = "library:"

// ChannelLogo is an image an admin uploaded to the logo library.
type ChannelLogo struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	File      string `json:"file"`
	CreatedAt int64  `json:"createdAt"`
	Channels  int    `json:"channels"` // how many channels use it
}

const logoCols = `l.id, l.name, l.file, l.created_at, (SELECT COUNT(*) FROM channels c WHERE c.logo = 'library:' || l.file)`

func scanLogo(sc interface{ Scan(...any) error }) (*ChannelLogo, error) {
	var l ChannelLogo
	if err := sc.Scan(&l.ID, &l.Name, &l.File, &l.CreatedAt, &l.Channels); err != nil {
		return nil, err
	}
	return &l, nil
}

// AddChannelLogo records an uploaded file. Uploading the same image again
// returns the existing entry (files are content-addressed).
func (s *Store) AddChannelLogo(name, file string) (*ChannelLogo, error) {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO channel_logos (name, file, created_at) VALUES (?, ?, ?)`, name, file, now()); err != nil {
		return nil, err
	}
	return scanLogo(s.db.QueryRow(`SELECT `+logoCols+` FROM channel_logos l WHERE l.file = ?`, file))
}

func (s *Store) ChannelLogos() ([]*ChannelLogo, error) {
	rows, err := s.db.Query(`SELECT ` + logoCols + ` FROM channel_logos l ORDER BY l.name COLLATE NOCASE, l.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ChannelLogo{}
	for rows.Next() {
		l, err := scanLogo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) ChannelLogo(id int64) (*ChannelLogo, error) {
	l, err := scanLogo(s.db.QueryRow(`SELECT `+logoCols+` FROM channel_logos l WHERE l.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}

// HasChannelLogoFile reports whether a library file is still in the library.
func (s *Store) HasChannelLogoFile(file string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM channel_logos WHERE file = ?`, file).Scan(&n)
	return n > 0
}

func (s *Store) RenameChannelLogo(id int64, name string) error {
	res, err := s.db.Exec(`UPDATE channel_logos SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// providerLogo is the SQL for a channel's own provider logo: its first stream's.
const providerLogo = `COALESCE((SELECT ls.logo FROM channel_streams cs JOIN live_streams ls ON ls.id = cs.stream_id
	WHERE cs.channel_id = channels.id ORDER BY cs.priority LIMIT 1), '')`

// SetLogoChannels makes exactly these channels use a library logo; channels
// that used it and aren't listed go back to their provider logo.
func (s *Store) SetLogoChannels(file string, channelIDs []int64) error {
	ref := LibraryLogoPrefix + file
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keep := map[int64]bool{}
	for _, id := range channelIDs {
		keep[id] = true
	}
	rows, err := tx.Query(`SELECT id FROM channels WHERE logo = ?`, ref)
	if err != nil {
		return err
	}
	var drop []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		if !keep[id] {
			drop = append(drop, id)
		}
	}
	rows.Close()
	for _, id := range drop {
		if _, err := tx.Exec(`UPDATE channels SET logo = `+providerLogo+` WHERE id = ?`, id); err != nil {
			return err
		}
	}
	for _, id := range channelIDs {
		if _, err := tx.Exec(`UPDATE channels SET logo = ? WHERE id = ?`, ref, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteChannelLogo removes a logo from the library. Channels that used it go
// back to their first stream's provider logo (or none).
func (s *Store) DeleteChannelLogo(id int64) (*ChannelLogo, error) {
	l, err := s.ChannelLogo(id)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE channels SET logo = `+providerLogo+` WHERE logo = ?`, LibraryLogoPrefix+l.File); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM channel_logos WHERE id = ?`, id); err != nil {
		return nil, err
	}
	return l, tx.Commit()
}

// SetChannelsLogo gives the channels a logo (a URL or a library reference).
func (s *Store) SetChannelsLogo(channelIDs []int64, logo string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range channelIDs {
		if _, err := tx.Exec(`UPDATE channels SET logo = ? WHERE id = ?`, logo, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ChannelCategory is a viewer's own grouping of channels ("Kids", "Sports I follow").
type ChannelCategory struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	ChannelIDs []int64 `json:"channelIds"`
}

// ChannelCategories lists the user's categories in their chosen order.
func (s *Store) ChannelCategories(uid int64) ([]*ChannelCategory, error) {
	rows, err := s.db.Query(`SELECT id, name FROM channel_categories WHERE user_id = ? ORDER BY position, id`, uid)
	if err != nil {
		return nil, err
	}
	out := []*ChannelCategory{}
	byID := map[int64]*ChannelCategory{}
	for rows.Next() {
		c := &ChannelCategory{ChannelIDs: []int64{}}
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
		byID[c.ID] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = s.db.Query(`SELECT m.category_id, m.channel_id FROM channel_category_members m
		JOIN channel_categories cat ON cat.id = m.category_id JOIN channels c ON c.id = m.channel_id
		WHERE cat.user_id = ? ORDER BY c.number, c.name COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cat, ch int64
		if err := rows.Scan(&cat, &ch); err != nil {
			return nil, err
		}
		if c := byID[cat]; c != nil {
			c.ChannelIDs = append(c.ChannelIDs, ch)
		}
	}
	return out, rows.Err()
}

// SaveChannelCategory creates (ID 0) or updates one of the user's categories
// and replaces its channels.
func (s *Store) SaveChannelCategory(uid int64, c *ChannelCategory) error {
	c.Name = strings.TrimSpace(c.Name)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if c.ID == 0 {
		res, err := tx.Exec(`INSERT INTO channel_categories (user_id, name, position, created_at)
			VALUES (?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM channel_categories WHERE user_id = ?), ?)`, uid, c.Name, uid, now())
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
	} else {
		res, err := tx.Exec(`UPDATE channel_categories SET name = ? WHERE id = ? AND user_id = ?`, c.Name, c.ID, uid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(`DELETE FROM channel_category_members WHERE category_id = ?`, c.ID); err != nil {
			return err
		}
	}
	for _, ch := range c.ChannelIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO channel_category_members (category_id, channel_id)
			SELECT ?, id FROM channels WHERE id = ?`, c.ID, ch); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetChannelInCategory adds a channel to, or removes it from, one of the user's categories.
func (s *Store) SetChannelInCategory(uid, categoryID, channelID int64, in bool) error {
	var owner int64
	err := s.db.QueryRow(`SELECT user_id FROM channel_categories WHERE id = ?`, categoryID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != uid) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if in {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO channel_category_members (category_id, channel_id) SELECT ?, id FROM channels WHERE id = ?`, categoryID, channelID)
	} else {
		_, err = s.db.Exec(`DELETE FROM channel_category_members WHERE category_id = ? AND channel_id = ?`, categoryID, channelID)
	}
	return err
}

func (s *Store) DeleteChannelCategory(uid, id int64) error {
	res, err := s.db.Exec(`DELETE FROM channel_categories WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// OrderChannelCategories sets the order of the user's categories; ids not listed keep their place after them.
func (s *Store) OrderChannelCategories(uid int64, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE channel_categories SET position = position + ? WHERE user_id = ?`, len(ids), uid); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE channel_categories SET position = ? WHERE id = ? AND user_id = ?`, i, id, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
