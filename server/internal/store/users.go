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
	PasswordHash string `json:"-"`
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash string, admin bool) (*User, error) {
	u := &User{Username: username, PasswordHash: passwordHash, IsAdmin: admin, CreatedAt: now()}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, boolInt(admin), u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.ID, _ = res.LastInsertId()
	return u, nil
}

const userCols = `id, username, is_admin, created_at, password_hash`

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	if err := r.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt, &u.PasswordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
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
	return out, rows.Err()
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
	var lastSeen int64
	var u User
	err := s.db.QueryRow(`SELECT u.id, u.username, u.is_admin, u.created_at, u.password_hash, s.last_seen
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ?`, h).
		Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt, &u.PasswordHash, &lastSeen)
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
	return &u, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (s *Store) DeleteUserSessions(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
