package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type TaskState struct {
	ID             string `json:"id"`
	Enabled        bool   `json:"enabled"`
	IntervalHours  int    `json:"intervalHours"`
	LastRun        int64  `json:"lastRun"`
	LastDurationMs int64  `json:"lastDurationMs"`
	LastResult     string `json:"lastResult,omitempty"`
	LastError      string `json:"lastError,omitempty"`
}

// TaskState returns the saved state, or ok=false if the task was never configured.
func (s *Store) TaskState(id string) (TaskState, bool, error) {
	t := TaskState{ID: id}
	err := s.db.QueryRow(`SELECT enabled, interval_hours, last_run, last_duration_ms, last_result, last_error FROM tasks WHERE id = ?`, id).
		Scan(&t.Enabled, &t.IntervalHours, &t.LastRun, &t.LastDurationMs, &t.LastResult, &t.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return t, false, nil
	}
	return t, err == nil, err
}

func (s *Store) SetTaskConfig(id string, enabled bool, intervalHours int) error {
	_, err := s.db.Exec(`INSERT INTO tasks (id, enabled, interval_hours) VALUES (?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET enabled = excluded.enabled, interval_hours = excluded.interval_hours`,
		id, boolInt(enabled), intervalHours)
	return err
}

// RecordTaskRun stores the outcome of a run. The row must exist (SetTaskConfig).
func (s *Store) RecordTaskRun(id string, started time.Time, result string, runErr error) error {
	msg := ""
	if runErr != nil {
		msg = runErr.Error()
	}
	_, err := s.db.Exec(`UPDATE tasks SET last_run = ?, last_duration_ms = ?, last_result = ?, last_error = ? WHERE id = ?`,
		started.Unix(), time.Since(started).Milliseconds(), result, msg, id)
	return err
}

// Backup writes a consistent copy of the live database to path.
func (s *Store) Backup(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// Optimize refreshes query planner statistics and compacts the file.
func (s *Store) Optimize() error {
	for _, q := range []string{`PRAGMA optimize`, `ANALYZE`, `VACUUM`, `PRAGMA wal_checkpoint(TRUNCATE)`} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// ReferencedImages lists every image reference in use (items, credits,
// collections), so cache cleanup knows what to keep.
func (s *Store) ReferencedImages() (map[string]bool, error) {
	refs := map[string]bool{}
	rows, err := s.db.Query(`SELECT poster, backdrop, logo, credits FROM items
		UNION ALL SELECT poster, backdrop, '', '[]' FROM collections`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var poster, backdrop, logo, credits string
		if err := rows.Scan(&poster, &backdrop, &logo, &credits); err != nil {
			return nil, err
		}
		for _, r := range []string{poster, backdrop, logo} {
			if r != "" {
				refs[r] = true
			}
		}
		if strings.Contains(credits, "tmdb:") {
			var cs []Credit
			json.Unmarshal([]byte(credits), &cs)
			for _, c := range cs {
				if c.Photo != "" {
					refs[c.Photo] = true
				}
			}
		}
	}
	return refs, rows.Err()
}

// TMDBArtwork lists the TMDB images titles and collections show (file
// names, newest titles first), for caching them before anyone asks.
func (s *Store) TMDBArtwork() (posters, backdrops, logos []string, err error) {
	rows, err := s.db.Query(`SELECT poster, backdrop, logo FROM (
		SELECT poster, backdrop, logo, added_at FROM items
		UNION ALL SELECT poster, backdrop, '', 0 FROM collections) ORDER BY added_at DESC`)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	add := func(list *[]string, ref string) {
		if f, ok := strings.CutPrefix(ref, "tmdb:/"); ok && f != "" && !seen[ref] {
			seen[ref] = true
			*list = append(*list, f)
		}
	}
	for rows.Next() {
		var poster, backdrop, logo string
		if err := rows.Scan(&poster, &backdrop, &logo); err != nil {
			return nil, nil, nil, err
		}
		add(&posters, poster)
		add(&backdrops, backdrop)
		add(&logos, logo)
	}
	return posters, backdrops, logos, rows.Err()
}

// FileSizes maps media file id → size, for validating cached subtitles.
func (s *Store) FileSizes() (map[int64]int64, error) {
	rows, err := s.db.Query(`SELECT id, size FROM media_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, size int64
		if err := rows.Scan(&id, &size); err != nil {
			return nil, err
		}
		out[id] = size
	}
	return out, rows.Err()
}

// UnprobedFiles lists files whose ffprobe analysis failed or never ran.
func (s *Store) UnprobedFiles(limit int) ([]*MediaFile, error) {
	rows, err := s.db.Query(`SELECT `+fileCols+` FROM media_files WHERE probed_at = 0 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*MediaFile{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// VideoFiles lists every media file, newest first.
func (s *Store) VideoFiles() ([]*MediaFile, error) {
	rows, err := s.db.Query(`SELECT ` + fileCols + ` FROM media_files ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*MediaFile{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UpdateStreams replaces a file's stream list (used when sidecar subtitles change).
func (s *Store) UpdateStreams(id int64, streams []Stream) error {
	b, _ := json.Marshal(streams)
	_, err := s.db.Exec(`UPDATE media_files SET streams = ? WHERE id = ?`, string(b), id)
	return err
}

// UpdateProbe saves fresh ffprobe results without touching scan bookkeeping.
func (s *Store) UpdateProbe(f *MediaFile) error {
	streams, _ := json.Marshal(f.Streams)
	_, err := s.db.Exec(`UPDATE media_files SET container = ?, duration = ?, bitrate = ?, video_codec = ?, width = ?, height = ?,
		bit_depth = ?, hdr = ?, audio_codec = ?, audio_channels = ?, streams = ?, probed_at = ? WHERE id = ?`,
		f.Container, f.Duration, f.Bitrate, f.VideoCodec, f.Width, f.Height, f.BitDepth, boolInt(f.HDR),
		f.AudioCodec, f.AudioChannels, string(streams), f.ProbedAt, f.ID)
	return err
}

// Restore replaces every row of the live database with the rows of the backup
// at path. The backup is migrated on a temporary copy first, so a backup from
// an older version works; one from a newer version is refused. The live file
// stays in place, so open connections and the rest of the server carry on.
//
// keepToken is the session of the admin doing the restore. It survives when
// the backup has the same admin, so they aren't signed out mid-task.
func (s *Store) Restore(ctx context.Context, path, keepToken string) error {
	tmp, err := os.CreateTemp("", "bingebandit-restore-*.db")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	if err := copyFile(path, tmpPath); err != nil {
		return err
	}
	if err := checkBackup(tmpPath); err != nil {
		return err
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Foreign keys can only be switched outside a transaction. Turn them back
	// on before the connection returns to the pool.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS bk`, tmpPath); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE bk`)

	tables, err := tableNames(ctx, conn, "main")
	if err != nil {
		return err
	}
	backupTables, err := tableNames(ctx, conn, "bk")
	if err != nil {
		return err
	}
	inBackup := map[string]bool{}
	for _, t := range backupTables {
		inBackup[t] = true
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var keep struct {
		userID               int64
		username, device, ip string
		created, lastSeen    int64
	}
	keepErr := tx.QueryRowContext(ctx, `SELECT s.user_id, u.username, s.device, s.ip, s.created_at, s.last_seen
		FROM main.sessions s JOIN main.users u ON u.id = s.user_id WHERE s.token_hash = ?`, hashToken(keepToken)).
		Scan(&keep.userID, &keep.username, &keep.device, &keep.ip, &keep.created, &keep.lastSeen)
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.`+quoteIdent(t)); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
		if !inBackup[t] {
			continue
		}
		cols, err := sharedColumns(ctx, tx, t)
		if err != nil {
			return err
		}
		list := strings.Join(cols, ", ")
		if _, err := tx.ExecContext(ctx, `INSERT INTO main.`+quoteIdent(t)+` (`+list+`) SELECT `+list+` FROM bk.`+quoteIdent(t)); err != nil {
			return fmt.Errorf("restore %s: %w", t, err)
		}
	}
	if keepErr == nil {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO main.sessions (token_hash, user_id, device, ip, created_at, last_seen)
			SELECT ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM main.users WHERE id = ? AND username = ? AND is_admin = 1)`,
			hashToken(keepToken), keep.userID, keep.device, keep.ip, keep.created, keep.lastSeen, keep.userID, keep.username); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.loadOverlayTag()
	return nil
}

// checkBackup migrates the copy at path and makes sure it is a sound
// BingeBandit database with an admin who can sign in.
func checkBackup(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	var version int
	err = db.QueryRow(`PRAGMA user_version`).Scan(&version)
	db.Close()
	if err != nil {
		return errors.New("not a BingeBandit backup")
	}
	if version > len(migrations) {
		return errors.New("this backup is from a newer version of BingeBandit; update the server first")
	}
	b, err := Open(path)
	if err != nil {
		return fmt.Errorf("not a BingeBandit backup: %w", err)
	}
	defer b.Close()
	var check string
	if err := b.db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		return errors.New("the backup is damaged")
	}
	var admins int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&admins); err != nil || admins == 0 {
		return errors.New("the backup has no admin account")
	}
	return nil
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// tableNames lists the tables of schema, including sqlite_sequence so
// AUTOINCREMENT counters come back too.
func tableNames(ctx context.Context, q queryer, schema string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT name FROM `+schema+`.sqlite_master
		WHERE type = 'table' AND (name NOT LIKE 'sqlite_%' OR name = 'sqlite_sequence') ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// sharedColumns lists the quoted columns that table has in both databases.
func sharedColumns(ctx context.Context, q queryer, table string) ([]string, error) {
	names := func(schema string) (map[string]bool, []string, error) {
		rows, err := q.QueryContext(ctx, `SELECT name FROM pragma_table_info(?, ?)`, table, schema)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		set, order := map[string]bool{}, []string{}
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return nil, nil, err
			}
			set[n] = true
			order = append(order, n)
		}
		return set, order, rows.Err()
	}
	_, mainCols, err := names("main")
	if err != nil {
		return nil, err
	}
	bkCols, _, err := names("bk")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range mainCols {
		if bkCols[c] {
			out = append(out, quoteIdent(c))
		}
	}
	return out, nil
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
