package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"bingebandit/internal/avatars"
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
	LiveTV        bool    `json:"liveTV"`               // may watch Live TV and use the DVR (admins always can)
	Kid           bool    `json:"kid"`                  // only sees Live TV channels approved for kids (never an admin)
	LibraryIDs    []int64 `json:"libraryIds,omitempty"` // only filled by ListUsers
	Passkeys      int     `json:"passkeys,omitempty"`   // how many; only filled by ListUsers
	HasPassword   bool    `json:"hasPassword"`          // false for accounts made by single sign-on
	HasPasscode   bool    `json:"hasPasscode"`          // settings are locked until the passcode is entered
	OwnerID       int64   `json:"ownerId,omitempty"`    // set for a profile: the account it belongs to
	ProfileName   string  `json:"profileName,omitempty"`
	Avatar        string  `json:"avatar,omitempty"`        // URL of the profile picture (a built-in one when none was uploaded)
	AvatarCustom  bool    `json:"avatarCustom,omitempty"`  // Avatar is an uploaded picture, not a built-in one
	AutoProfileID int64   `json:"autoProfileId,omitempty"` // an account's member to open as without asking
	Email         string  `json:"email,omitempty"`         // a single sign-on with this email signs in as this user
	PasswordHash  string  `json:"-"`
	PasscodeHash  string  `json:"-"`
	TOTPSecret    string  `json:"-"`
}

// SetAvatar stores a user's profile picture as an artwork reference
// ("local:/<file>") or a built-in one ("avatar:/<name>"); "" goes back to
// the built-in picture picked from the user's id.
func (s *Store) SetAvatar(userID int64, ref string) error {
	res, err := s.db.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, ref, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAutoProfile sets the member an account opens as without asking who's
// watching; 0 asks every time.
func (s *Store) SetAutoProfile(accountID, memberID int64) error {
	_, err := s.db.Exec(`UPDATE users SET auto_profile_id = ? WHERE id = ? AND owner_id IS NULL`, memberID, accountID)
	return err
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

// HouseholdID is the account a user belongs to: its owner for a profile,
// otherwise itself.
func (u *User) HouseholdID() int64 {
	if u.OwnerID != 0 {
		return u.OwnerID
	}
	return u.ID
}

// ProfileUsername is the unique username stored for a profile: its name and
// the account's, so two accounts can both have a "Kids" profile.
func ProfileUsername(name, owner string) string { return name + " (" + owner + ")" }

// CreateProfile adds a profile under owner. It starts with no password and
// sees the same libraries as the owner.
func (s *Store) CreateProfile(owner *User, name string) (*User, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u := &User{Username: ProfileUsername(name, owner.Username), CreatedAt: now(), AllLibraries: owner.IsAdmin || owner.AllLibraries,
		OwnerID: owner.ID, ProfileName: name, LiveTV: owner.LiveTV}
	res, err := tx.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at, all_libraries, owner_id, profile_name, live_tv) VALUES (?, '', 0, ?, ?, ?, ?, ?)`,
		u.Username, u.CreatedAt, boolInt(u.AllLibraries), owner.ID, name, boolInt(u.LiveTV))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrExists
		}
		return nil, err
	}
	u.ID, _ = res.LastInsertId()
	if !u.AllLibraries {
		if _, err := tx.Exec(`INSERT INTO user_libraries (user_id, library_id) SELECT ?, library_id FROM user_libraries WHERE user_id = ?`, u.ID, owner.ID); err != nil {
			return nil, err
		}
	}
	return u, tx.Commit()
}

// RenameProfile changes a profile's name (and so its stored username).
func (s *Store) RenameProfile(id int64, name, owner string) error {
	_, err := s.db.Exec(`UPDATE users SET profile_name = ?, username = ? WHERE id = ? AND owner_id IS NOT NULL`, name, ProfileUsername(name, owner), id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	return err
}

// Household lists an account and its profiles, the account first.
func (s *Store) Household(ownerID int64) ([]*User, error) {
	rows, err := s.db.Query(`SELECT `+userCols+` FROM users WHERE id = ? OR owner_id = ? ORDER BY owner_id IS NOT NULL, created_at, id`, ownerID, ownerID)
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

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, passwordHash string, admin bool) (*User, error) {
	u := &User{Username: username, PasswordHash: passwordHash, IsAdmin: admin, CreatedAt: now(), AllLibraries: true, LiveTV: true, HasPassword: passwordHash != ""}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)`,
		username, passwordHash, boolInt(admin), u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.ID, _ = res.LastInsertId()
	return u, nil
}

const userCols = `id, username, is_admin, created_at, all_libraries, profile_public, totp_enabled, share_activity, password_hash, totp_secret, passcode_hash, COALESCE(owner_id, 0), profile_name, avatar, auto_profile_id, live_tv, kid, email`

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	var avatar string
	if err := r.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt, &u.AllLibraries, &u.ProfilePublic, &u.TOTPEnabled, &u.ShareActivity,
		&u.PasswordHash, &u.TOTPSecret, &u.PasscodeHash, &u.OwnerID, &u.ProfileName, &avatar, &u.AutoProfileID, &u.LiveTV, &u.Kid, &u.Email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.HasPassword = u.PasswordHash != ""
	u.HasPasscode = u.PasscodeHash != ""
	switch {
	case avatar == "":
		u.Avatar = avatars.URL(avatars.ForUser(u.ID))
	case strings.HasPrefix(avatar, avatars.RefPrefix):
		u.Avatar = ImageURL(avatar)
	default:
		u.Avatar, u.AvatarCustom = ImageURL(avatar), true
	}
	u.LiveTV = u.LiveTV || u.IsAdmin
	u.Kid = u.Kid && !u.IsAdmin
	return &u, nil
}

// SetKid marks a user as a kid (or not): kids only see the Live TV channels
// approved for kids.
func (s *Store) SetKid(userID int64, on bool) error {
	res, err := s.db.Exec(`UPDATE users SET kid = ? WHERE id = ?`, boolInt(on), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetLiveTV turns Live TV on or off for a user and the profiles under it.
func (s *Store) SetLiveTV(userID int64, on bool) error {
	res, err := s.db.Exec(`UPDATE users SET live_tv = ? WHERE id = ? OR owner_id = ?`, boolInt(on), userID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UserByUsername(username string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

// UserByEmail finds the user an admin attached email to.
func (s *Store) UserByEmail(email string) (*User, error) {
	if email == "" {
		return nil, ErrNotFound
	}
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE email = ? COLLATE NOCASE`, email))
}

// SetEmail attaches an email to a user for single sign-on; "" removes it.
func (s *Store) SetEmail(userID int64, email string) error {
	res, err := s.db.Exec(`UPDATE users SET email = ? WHERE id = ?`, email, userID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrExists
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM passkeys WHERE user_id = ?`, u.ID).Scan(&u.Passkeys); err != nil {
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
