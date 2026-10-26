package store

import "strings"

// LinkUser moves account u under account owner as a profile called name,
// keeping its watch history, ratings, pins and settings. It loses its
// password, two-step sign-in and passkeys and is signed out everywhere,
// since profiles are reached through their account. Single sign-on stays
// and now lands in the profile. Its settings passcode stays as its PIN. Profiles u already had
// move with it under owner.
func (s *Store) LinkUser(u, owner *User, name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	moved := []int64{u.ID}
	rows, err := tx.Query(`SELECT id, profile_name FROM users WHERE owner_id = ?`, u.ID)
	if err != nil {
		return err
	}
	type profile struct {
		id   int64
		name string
	}
	var profiles []profile
	for rows.Next() {
		var p profile
		if err := rows.Scan(&p.id, &p.name); err != nil {
			rows.Close()
			return err
		}
		profiles = append(profiles, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET owner_id = ?, profile_name = ?, username = ?, password_hash = '', totp_enabled = 0,
		totp_secret = '', recovery_codes = '[]', auto_profile_id = 0 WHERE id = ?`,
		owner.ID, name, ProfileUsername(name, owner.Username), u.ID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrExists
		}
		return err
	}
	for _, p := range profiles {
		if _, err := tx.Exec(`UPDATE users SET owner_id = ?, username = ? WHERE id = ?`, owner.ID, ProfileUsername(p.name, owner.Username), p.id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrExists
			}
			return err
		}
		moved = append(moved, p.id)
	}
	if _, err := tx.Exec(`DELETE FROM passkeys WHERE user_id = ?`, u.ID); err != nil {
		return err
	}
	for _, id := range moved {
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// UnlinkProfile makes profile p an account of its own again, signing in as
// username with passwordHash. It keeps everything else and is signed out
// everywhere.
func (s *Store) UnlinkProfile(p *User, username, passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET owner_id = NULL, profile_name = '', username = ?, password_hash = ? WHERE id = ?`,
		username, passwordHash, p.ID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrExists
		}
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET auto_profile_id = 0 WHERE id = ? AND auto_profile_id = ?`, p.OwnerID, p.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, p.ID); err != nil {
		return err
	}
	return tx.Commit()
}
