package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ---- two-factor authentication ----

// SetTOTPSecret stores a not-yet-confirmed secret during 2FA setup.
func (s *Store) SetTOTPSecret(userID int64, secret string) error {
	_, err := s.db.Exec(`UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE id = ?`, secret, userID)
	return err
}

// EnableTOTP turns 2FA on and stores hashes of the recovery codes.
func (s *Store) EnableTOTP(userID int64, recoveryCodes []string) error {
	hashes := make([]string, len(recoveryCodes))
	for i, c := range recoveryCodes {
		hashes[i] = hashToken(normalizeRecovery(c))
	}
	b, _ := json.Marshal(hashes)
	_, err := s.db.Exec(`UPDATE users SET totp_enabled = 1, recovery_codes = ? WHERE id = ? AND totp_secret != ''`, string(b), userID)
	return err
}

func (s *Store) DisableTOTP(userID int64) error {
	_, err := s.db.Exec(`UPDATE users SET totp_enabled = 0, totp_secret = '', recovery_codes = '[]' WHERE id = ?`, userID)
	return err
}

func normalizeRecovery(c string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(c))
}

// UseRecoveryCode consumes a recovery code; each works once.
func (s *Store) UseRecoveryCode(userID int64, code string) bool {
	var raw string
	if s.db.QueryRow(`SELECT recovery_codes FROM users WHERE id = ?`, userID).Scan(&raw) != nil {
		return false
	}
	var hashes []string
	json.Unmarshal([]byte(raw), &hashes)
	h := hashToken(normalizeRecovery(code))
	for i, x := range hashes {
		if x == h {
			hashes = append(hashes[:i], hashes[i+1:]...)
			b, _ := json.Marshal(hashes)
			s.db.Exec(`UPDATE users SET recovery_codes = ? WHERE id = ?`, string(b), userID)
			return true
		}
	}
	return false
}

func (s *Store) SetShareActivity(userID int64, share bool) error {
	_, err := s.db.Exec(`UPDATE users SET share_activity = ? WHERE id = ?`, boolInt(share), userID)
	return err
}

// ---- API keys ----

type APIKey struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Prefix    string `json:"prefix"` // first characters, to recognise a key
	CreatedAt int64  `json:"createdAt"`
	LastUsed  int64  `json:"lastUsed"`
}

// APIKeyPrefix marks BingeBandit keys so they're easy to spot (and to scan for in leaks).
const APIKeyPrefix = "bb_"

// CreateAPIKey returns the new key; only its hash is stored, so it can't be shown again.
func (s *Store) CreateAPIKey(userID int64, name string) (string, *APIKey, error) {
	b := make([]byte, 24)
	rand.Read(b)
	key := APIKeyPrefix + hex.EncodeToString(b)
	k := &APIKey{Name: name, Prefix: key[:9], CreatedAt: now()}
	res, err := s.db.Exec(`INSERT INTO api_keys (user_id, name, key_hash, prefix, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, name, hashToken(key), k.Prefix, k.CreatedAt)
	if err != nil {
		return "", nil, err
	}
	k.ID, _ = res.LastInsertId()
	return key, k, nil
}

func (s *Store) APIKeys(userID int64) ([]APIKey, error) {
	rows, err := s.db.Query(`SELECT id, name, prefix, created_at, last_used FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedAt, &k.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByAPIKey authenticates a request carrying an API key.
func (s *Store) UserByAPIKey(key string) (*User, error) {
	if !strings.HasPrefix(key, APIKeyPrefix) {
		return nil, ErrNotFound
	}
	h := hashToken(key)
	var uid, lastUsed int64
	err := s.db.QueryRow(`SELECT user_id, last_used FROM api_keys WHERE key_hash = ?`, h).Scan(&uid, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if t := now(); t-lastUsed > 300 {
		s.db.Exec(`UPDATE api_keys SET last_used = ? WHERE key_hash = ?`, t, h)
	}
	return s.UserByID(uid)
}

// ---- webhooks ----

type Webhook struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Events     []string `json:"events"`
	Format     string   `json:"format"` // "" = BingeBandit JSON, "plex" = Plex-style multipart (Yamtrack and other Plex webhook receivers)
	Secret     string   `json:"secret,omitempty"`
	Enabled    bool     `json:"enabled"`
	LastStatus string   `json:"lastStatus,omitempty"`
	LastAt     int64    `json:"lastAt"`
}

func (s *Store) Webhooks() ([]*Webhook, error) {
	rows, err := s.db.Query(`SELECT id, name, url, events, format, secret, enabled, last_status, last_at FROM webhooks ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Webhook{}
	for rows.Next() {
		var w Webhook
		var ev string
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &ev, &w.Format, &w.Secret, &w.Enabled, &w.LastStatus, &w.LastAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(ev), &w.Events)
		out = append(out, &w)
	}
	return out, rows.Err()
}

func (s *Store) SaveWebhook(w *Webhook) error {
	ev, _ := json.Marshal(nonNil(w.Events))
	if w.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO webhooks (name, url, events, format, secret, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			w.Name, w.URL, string(ev), w.Format, w.Secret, boolInt(w.Enabled), now())
		if err != nil {
			return err
		}
		w.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.db.Exec(`UPDATE webhooks SET name = ?, url = ?, events = ?, format = ?, secret = ?, enabled = ? WHERE id = ?`,
		w.Name, w.URL, string(ev), w.Format, w.Secret, boolInt(w.Enabled), w.ID)
	return err
}

func (s *Store) DeleteWebhook(id int64) error {
	_, err := s.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

func (s *Store) SetWebhookResult(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE webhooks SET last_status = ?, last_at = ? WHERE id = ?`, status, now(), id)
	return err
}
