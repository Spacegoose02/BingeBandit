package store

import (
	"database/sql"
	"errors"
	"strings"
)

// TraktAccount is a user's connection to Trakt.
type TraktAccount struct {
	UserID       int64  `json:"-"`
	Username     string `json:"username"`
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
	ExpiresAt    int64  `json:"-"`
	AutoSync     bool   `json:"autoSync"`
	ConnectedAt  int64  `json:"connectedAt"`
	LastSync     int64  `json:"lastSync"`
	LastError    string `json:"lastError,omitempty"`
}

func (s *Store) TraktAccount(uid int64) (*TraktAccount, error) {
	var a TraktAccount
	err := s.db.QueryRow(`SELECT user_id, username, access_token, refresh_token, expires_at, auto_sync, connected_at, last_sync, last_error
		FROM trakt_accounts WHERE user_id = ?`, uid).Scan(&a.UserID, &a.Username, &a.AccessToken, &a.RefreshToken, &a.ExpiresAt,
		&a.AutoSync, &a.ConnectedAt, &a.LastSync, &a.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

// SaveTraktAccount stores a new connection or refreshed tokens.
func (s *Store) SaveTraktAccount(a *TraktAccount) error {
	if a.ConnectedAt == 0 {
		a.ConnectedAt = now()
	}
	_, err := s.db.Exec(`INSERT INTO trakt_accounts (user_id, username, access_token, refresh_token, expires_at, auto_sync, connected_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET username = excluded.username, access_token = excluded.access_token,
			refresh_token = excluded.refresh_token, expires_at = excluded.expires_at`,
		a.UserID, a.Username, a.AccessToken, a.RefreshToken, a.ExpiresAt, boolInt(a.AutoSync), a.ConnectedAt)
	return err
}

func (s *Store) DeleteTraktAccount(uid int64) error {
	_, err := s.db.Exec(`DELETE FROM trakt_accounts WHERE user_id = ?`, uid)
	return err
}

func (s *Store) SetTraktAutoSync(uid int64, on bool) error {
	_, err := s.db.Exec(`UPDATE trakt_accounts SET auto_sync = ? WHERE user_id = ?`, boolInt(on), uid)
	return err
}

func (s *Store) SetTraktResult(uid int64, errMsg string) error {
	_, err := s.db.Exec(`UPDATE trakt_accounts SET last_sync = ?, last_error = ? WHERE user_id = ?`, now(), errMsg, uid)
	return err
}

// WatchedPlay is a finished play with the ids trackers need.
type WatchedPlay struct {
	PlayID       int64
	Kind         string // movie | episode
	WatchedAt    int64
	Title        string // movie or show title
	Year         int    // movie or show year
	TMDBID       int64  // movie or show TMDB id
	Season       int
	Episode      int
	EpisodeTitle string
	Completed    bool
	StartedAt    int64
}

const watchedPlayCols = `p.id, it.kind, p.stopped_at, COALESCE(sh.title, it.title), COALESCE(sh.year, it.year),
	COALESCE(sh.tmdb_id, it.tmdb_id), COALESCE(it.season_number, 0), COALESCE(it.episode_number, 0),
	CASE WHEN it.kind = 'episode' THEN it.title ELSE '' END, p.completed, p.started_at`

const watchedPlayFrom = `FROM plays p JOIN items it ON it.id = p.item_id LEFT JOIN items sh ON sh.id = it.show_id`

func scanWatchedPlays(rows *sql.Rows) ([]WatchedPlay, error) {
	defer rows.Close()
	var out []WatchedPlay
	for rows.Next() {
		var w WatchedPlay
		if err := rows.Scan(&w.PlayID, &w.Kind, &w.WatchedAt, &w.Title, &w.Year, &w.TMDBID, &w.Season, &w.Episode, &w.EpisodeTitle,
			&w.Completed, &w.StartedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// PendingTraktPlays lists finished plays not yet sent to Trakt, started at or after since.
func (s *Store) PendingTraktPlays(uid, since int64, limit int) ([]WatchedPlay, error) {
	rows, err := s.db.Query(`SELECT `+watchedPlayCols+` `+watchedPlayFrom+`
		WHERE p.user_id = ? AND p.completed = 1 AND p.trakt_at = 0 AND p.started_at >= ? AND it.kind IN ('movie', 'episode')
		ORDER BY p.stopped_at LIMIT ?`, uid, since, limit)
	if err != nil {
		return nil, err
	}
	return scanWatchedPlays(rows)
}

// CountPendingTrakt counts finished plays not yet on Trakt, started at or after since.
func (s *Store) CountPendingTrakt(uid, since int64) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) `+watchedPlayFrom+` WHERE p.user_id = ? AND p.completed = 1 AND p.trakt_at = 0 AND p.started_at >= ?
		AND it.kind IN ('movie', 'episode')`, uid, since).Scan(&n)
	return n
}

func (s *Store) MarkTraktSent(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	q := `UPDATE plays SET trakt_at = ? WHERE id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	args := []any{now()}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.Exec(q, args...)
	return err
}

// WatchedPlays lists a user's plays of items still in the library, newest first (for exports).
func (s *Store) WatchedPlays(uid int64) ([]WatchedPlay, error) {
	rows, err := s.db.Query(`SELECT `+watchedPlayCols+` `+watchedPlayFrom+`
		WHERE p.user_id = ? AND it.kind IN ('movie', 'episode') ORDER BY p.stopped_at DESC`, uid)
	if err != nil {
		return nil, err
	}
	return scanWatchedPlays(rows)
}
