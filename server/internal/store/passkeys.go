package store

import (
	"database/sql"
	"errors"
)

// Passkey is a WebAuthn credential that signs a user in without a password.
type Passkey struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"-"`
	CredentialID []byte `json:"-"`
	Credential   string `json:"-"` // the WebAuthn library's JSON for the credential
	Name         string `json:"name"`
	CreatedAt    int64  `json:"createdAt"`
	LastUsedAt   int64  `json:"lastUsedAt"`
}

const passkeyCols = `id, user_id, credential_id, credential, name, created_at, last_used_at`

func scanPasskey(row interface{ Scan(...any) error }) (*Passkey, error) {
	var p Passkey
	err := row.Scan(&p.ID, &p.UserID, &p.CredentialID, &p.Credential, &p.Name, &p.CreatedAt, &p.LastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (s *Store) AddPasskey(p *Passkey) error {
	p.CreatedAt = now()
	res, err := s.db.Exec(`INSERT INTO passkeys (user_id, credential_id, credential, name, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.UserID, p.CredentialID, p.Credential, p.Name, p.CreatedAt)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// Passkeys lists a user's passkeys, oldest first.
func (s *Store) Passkeys(userID int64) ([]*Passkey, error) {
	rows, err := s.db.Query(`SELECT `+passkeyCols+` FROM passkeys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Passkey{}
	for rows.Next() {
		p, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PasskeyByCredentialID(id []byte) (*Passkey, error) {
	return scanPasskey(s.db.QueryRow(`SELECT `+passkeyCols+` FROM passkeys WHERE credential_id = ?`, id))
}

// UsePasskey saves the credential after a sign-in (its sign count changes).
func (s *Store) UsePasskey(id int64, credential string) error {
	_, err := s.db.Exec(`UPDATE passkeys SET credential = ?, last_used_at = ? WHERE id = ?`, credential, now(), id)
	return err
}

func (s *Store) RenamePasskey(userID, id int64, name string) error {
	res, err := s.db.Exec(`UPDATE passkeys SET name = ? WHERE id = ? AND user_id = ?`, name, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasskey removes one of the user's passkeys; userID 0 removes any (admin).
func (s *Store) DeletePasskey(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM passkeys WHERE id = ? AND (? = 0 OR user_id = ?)`, id, userID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePasskeys removes all of a user's passkeys (an admin helping someone locked out).
func (s *Store) DeletePasskeys(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM passkeys WHERE user_id = ?`, userID)
	return err
}
