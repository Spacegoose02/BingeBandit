package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
)

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	IsAdmin      bool   `json:"isAdmin"`
	CreatedAt    int64  `json:"createdAt"`
	AllLibraries bool   `json:"allLibraries"`
	// ProfilePublic lets other users of this server see the profile and stats.
	ProfilePublic bool    `json:"profilePublic"`
	TOTPEnabled   bool    `json:"totpEnabled"`
	ShareActivity bool    `json:"shareActivity"`        // others may see what this user is watching
	LibraryIDs    []int64 `json:"libraryIds,omitempty"` // only filled by ListUsers
	HasPassword   bool    `json:"hasPassword"`          // false for accounts made by single sign-on
	HasPasscode   bool    `json:"hasPasscode"`          // settings are locked until the passcode is entered
	PasswordHash  string  `json:"-"`
	PasscodeHash  string  `json:"-"`
	TOTPSecret    string  `json:"-"`
}

// SetPasscode stores a bcrypt hash of the user's settings passcode; "" removes it.
func (s *Store) SetPasscode(userID int64, hash string) error {
	res, err := s.db.Exec(`UPDATE users SET passcode_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash string, admin bool) (*User, error) {
	u := &User{Username: username, PasswordHash: passwordHash, IsAdmin: admin, CreatedAt: now(), AllLibraries: true, HasPassword: passwordHash != ""}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, boolInt(admin), u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.ID, _ = res.LastInsertId()
	return u, nil
}

const userCols = `id, username, is_admin, created_at, all_libraries, profile_public, totp_enabled, share_activity, password_hash, totp_secret, passcode_hash`

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := r.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt, &u.AllLibraries, &u.ProfilePublic, &u.TOTPEnabled, &u.ShareActivity,
		&u.PasswordHash, &u.TOTPSecret, &u.PasscodeHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.HasPassword = u.PasswordHash != ""
	u.HasPasscode = u.PasscodeHash != ""
	return &u, nil
}

func (s *Store) UserByUsername(username string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, u := range out {
		if u.LibraryIDs, err = s.UserLibraryIDs(u.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) UserLibraryIDs(userID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT library_id FROM user_libraries WHERE user_id = ? ORDER BY library_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetUserLibraries sets which libraries a user sees. all=true also covers
// libraries added later.
func (s *Store) SetUserLibraries(userID int64, all bool, libraryIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET all_libraries = ? WHERE id = ?`, boolInt(all), userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_libraries WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, id := range libraryIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO user_libraries (user_id, library_id) VALUES (?, ?)`, userID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetProfilePublic(userID int64, public bool) error {
	_, err := s.db.Exec(`UPDATE users SET profile_public = ? WHERE id = ?`, boolInt(public), userID)
	return err
}

// CanSeeLibrary reports whether u may browse the library.
func (s *Store) CanSeeLibrary(u *User, libraryID int64) bool {
	if u.IsAdmin || u.AllLibraries {
		return true
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM user_libraries WHERE user_id = ? AND library_id = ?`, u.ID, libraryID).Scan(&n)
	return n > 0
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) SetPassword(id int64, passwordHash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

// Sessions store only a hash of the bearer token, so a leaked database
// does not hand out live logins.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) CreateSession(token string, userID int64, device, ip string) error {
	t := now()
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, device, ip, created_at, last_seen) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), userID, device, ip, t, t)
	return err
}

func (s *Store) UserBySession(token string) (*User, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	h := hashToken(token)
	var uid, lastSeen int64
	err := s.db.QueryRow(`SELECT user_id, last_seen FROM sessions WHERE token_hash = ?`, h).Scan(&uid, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// Avoid a write on every request; minute-level freshness is plenty.
	if t := now(); t-lastSeen > 300 {
		s.db.Exec(`UPDATE sessions SET last_seen = ? WHERE token_hash = ?`, t, h)
	}
	return s.UserByID(uid)
}

// Session is a signed-in device. ID is a short prefix of the token hash,
// enough to identify it without revealing anything usable.
type Session struct {
	ID        string `json:"id"`
	Device    string `json:"device"`
	IP        string `json:"ip"`
	CreatedAt int64  `json:"createdAt"`
	LastSeen  int64  `json:"lastSeen"`
	Current   bool   `json:"current"`
}

func (s *Store) Sessions(userID int64, currentToken string) ([]Session, error) {
	cur := hashToken(currentToken)
	rows, err := s.db.Query(`SELECT token_hash, device, ip, created_at, last_seen FROM sessions WHERE user_id = ? ORDER BY last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var h string
		var se Session
		if err := rows.Scan(&h, &se.Device, &se.IP, &se.CreatedAt, &se.LastSeen); err != nil {
			return nil, err
		}
		se.ID, se.Current = h[:16], h == cur
		out = append(out, se)
	}
	return out, rows.Err()
}

// RevokeSession signs out one of a user's devices by its Session.ID.
func (s *Store) RevokeSession(userID int64, id string) error {
	if len(id) != 16 {
		return ErrNotFound
	}
	res, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND substr(token_hash, 1, 16) = ?`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (s *Store) DeleteUserSessions(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
