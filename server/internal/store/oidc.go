package store

import (
	"database/sql"
	"errors"
)

// OIDCProvider is an OpenID Connect identity provider people can sign in
// with (Google, TinyAuth, Authelia, Authentik, Keycloak…).
type OIDCProvider struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"` // write-only: never returned by the API
	HasSecret    bool   `json:"hasSecret"`
	Scopes       string `json:"scopes"`     // extra scopes beyond "openid profile email"
	AutoCreate   bool   `json:"autoCreate"` // make an account on first sign-in instead of requiring a link
	Enabled      bool   `json:"enabled"`
}

const oidcCols = `id, name, issuer, client_id, client_secret, scopes, auto_create, enabled`

func scanOIDC(r interface{ Scan(...any) error }) (*OIDCProvider, error) {
	var p OIDCProvider
	if err := r.Scan(&p.ID, &p.Name, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.Scopes, &p.AutoCreate, &p.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.HasSecret = p.ClientSecret != ""
	return &p, nil
}

func (s *Store) OIDCProviders() ([]*OIDCProvider, error) {
	rows, err := s.db.Query(`SELECT ` + oidcCols + ` FROM oidc_providers ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*OIDCProvider{}
	for rows.Next() {
		p, err := scanOIDC(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) OIDCProvider(id int64) (*OIDCProvider, error) {
	return scanOIDC(s.db.QueryRow(`SELECT `+oidcCols+` FROM oidc_providers WHERE id = ?`, id))
}

// SaveOIDCProvider inserts or updates a provider. On update an empty secret
// keeps the stored one, so the admin UI never needs to see it.
func (s *Store) SaveOIDCProvider(p *OIDCProvider) error {
	if p.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO oidc_providers (name, issuer, client_id, client_secret, scopes, auto_create, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Name, p.Issuer, p.ClientID, p.ClientSecret, p.Scopes, boolInt(p.AutoCreate), boolInt(p.Enabled), now())
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.db.Exec(`UPDATE oidc_providers SET name = ?, issuer = ?, client_id = ?,
		client_secret = CASE WHEN ? = '' THEN client_secret ELSE ? END, scopes = ?, auto_create = ?, enabled = ? WHERE id = ?`,
		p.Name, p.Issuer, p.ClientID, p.ClientSecret, p.ClientSecret, p.Scopes, boolInt(p.AutoCreate), boolInt(p.Enabled), p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteOIDCProvider(id int64) error {
	_, err := s.db.Exec(`DELETE FROM oidc_providers WHERE id = ?`, id)
	return err
}

// Identity links a user to an account at an OIDC provider.
type Identity struct {
	ID           int64  `json:"id"`
	ProviderID   int64  `json:"providerId"`
	ProviderName string `json:"providerName"`
	Subject      string `json:"-"`
	Email        string `json:"email"`
	CreatedAt    int64  `json:"createdAt"`
	LastUsed     int64  `json:"lastUsed"`
}

func (s *Store) Identities(userID int64) ([]*Identity, error) {
	rows, err := s.db.Query(`SELECT i.id, i.provider_id, p.name, i.subject, i.email, i.created_at, i.last_used
		FROM user_identities i JOIN oidc_providers p ON p.id = i.provider_id WHERE i.user_id = ? ORDER BY p.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Identity{}
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.ID, &i.ProviderID, &i.ProviderName, &i.Subject, &i.Email, &i.CreatedAt, &i.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, &i)
	}
	return out, rows.Err()
}

// UserByIdentity finds the user linked to a provider account and marks the link used.
func (s *Store) UserByIdentity(providerID int64, subject, email string) (*User, error) {
	var uid int64
	if err := s.db.QueryRow(`SELECT user_id FROM user_identities WHERE provider_id = ? AND subject = ?`, providerID, subject).Scan(&uid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.db.Exec(`UPDATE user_identities SET last_used = ?, email = CASE WHEN ? = '' THEN email ELSE ? END WHERE provider_id = ? AND subject = ?`,
		now(), email, email, providerID, subject)
	return s.UserByID(uid)
}

// ErrIdentityTaken means the provider account is already linked to someone else.
var ErrIdentityTaken = errors.New("that account is already linked to another BingeBandit user")

func (s *Store) LinkIdentity(userID, providerID int64, subject, email string) error {
	var owner int64
	err := s.db.QueryRow(`SELECT user_id FROM user_identities WHERE provider_id = ? AND subject = ?`, providerID, subject).Scan(&owner)
	switch {
	case err == nil && owner != userID:
		return ErrIdentityTaken
	case err == nil:
		return nil // already linked to this user
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	_, err = s.db.Exec(`INSERT INTO user_identities (user_id, provider_id, subject, email, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, providerID, subject, email, now())
	return err
}

func (s *Store) UnlinkIdentity(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM user_identities WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
