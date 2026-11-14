package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	s.heavy.Lock()
	defer s.heavy.Unlock()
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// IsBusy reports whether err is SQLite saying another connection holds the
// lock it needs (SQLITE_BUSY or SQLITE_LOCKED, any extended code).
func IsBusy(err error) bool {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		c := coded.Code() & 0xff
		return c == 5 || c == 6
	}
	return err != nil && (strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked"))
}

// optimizeBusyTimeout is how long one VACUUM waits for the write lock, and
// optimizeRetries how often it's tried again after that, so a burst of
// writes from a scan or another maintenance task can't fail the run.
var (
	optimizeBusyTimeout = 2 * time.Minute
	optimizeRetries     = []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute}
)

// Optimize refreshes query planner statistics and compacts the file. VACUUM
// needs the database to itself for a moment; if other work keeps it busy
// past the retries, compaction is skipped (statistics are still updated)
// and the result says so, rather than failing the run.
func (s *Store) Optimize(ctx context.Context) (string, error) {
	s.heavy.Lock()
	defer s.heavy.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout = %d`, optimizeBusyTimeout.Milliseconds())); err != nil {
		return "", err
	}
	// Back to the pool's usual timeout before this connection is reused.
	defer conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA busy_timeout = 10000`)

	retry := func(q string) error {
		for attempt := 0; ; attempt++ {
			_, err := conn.ExecContext(ctx, q)
			if !IsBusy(err) || attempt >= len(optimizeRetries) {
				return err
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(optimizeRetries[attempt]):
			}
		}
	}
	// A damaged index makes some queries fail with "database disk image is
	// malformed" while the rest work, e.g. every season of every show
	// reading as missing. Rebuilding the indexes from the tables fixes
	// that; damage to the tables themselves needs a backup.
	repaired := false
	if problem := integrityProblem(ctx, conn); problem != "" {
		slog.Warn("database damaged; rebuilding its indexes", "problem", problem)
		if err := rebuildIndexes(ctx, conn, retry); err != nil {
			return "", fmt.Errorf("the database is damaged (%s) and rebuilding its indexes failed (%v); restore a backup", problem, err)
		}
		if still := integrityProblem(ctx, conn); still != "" {
			return "", fmt.Errorf("the database is damaged (%s) and rebuilding its indexes didn't fix it; restore a backup", still)
		}
		repaired = true
	}
	for _, q := range []string{`PRAGMA optimize`, `ANALYZE`} {
		if err := retry(q); err != nil {
			return "", fmt.Errorf("%s: %w", q, err)
		}
	}
	result := "updated statistics and compacted the database"
	if err := retry(`VACUUM`); err != nil {
		if !IsBusy(err) {
			return "", fmt.Errorf("VACUUM: %w", err)
		}
		result = "updated statistics; the database stayed busy, so compacting waits for the next run"
	}
	// Reports busy in its result row rather than failing; a partial
	// checkpoint is fine, the next one finishes it.
	conn.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	if repaired {
		result = "repaired damaged indexes, " + result
	}
	return result, nil
}

// rebuildIndexes recreates every index from its table. REINDEX and DROP
// INDEX can't when an index's pages are damaged (both read them to free
// them), so the indexes are taken out of the schema directly, VACUUM copies
// the tables to a fresh file (leaving the damaged pages behind), and the
// indexes are created again from their saved definitions.
//
// It only goes ahead when every table reads in full without its indexes:
// damage in a table needs a backup, and the schema is left as it was.
func rebuildIndexes(ctx context.Context, conn *sql.Conn, retry func(string) error) error {
	tables, err := conn.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			tables.Close()
			return err
		}
		names = append(names, name)
	}
	tables.Close()
	if err := tables.Err(); err != nil {
		return err
	}
	for _, name := range names {
		var n int64
		q := `SELECT COUNT(*) FROM "` + strings.ReplaceAll(name, `"`, `""`) + `" NOT INDEXED`
		if err := conn.QueryRowContext(ctx, q).Scan(&n); err != nil {
			return fmt.Errorf("table %s is damaged too: %w", name, err)
		}
	}

	rows, err := conn.QueryContext(ctx, `SELECT name, sql FROM sqlite_schema WHERE type = 'index' AND sql IS NOT NULL`)
	if err != nil {
		return err
	}
	type index struct{ name, sql string }
	var indexes []index
	for rows.Next() {
		var ix index
		if err := rows.Scan(&ix.name, &ix.sql); err != nil {
			rows.Close()
			return err
		}
		indexes = append(indexes, ix)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA writable_schema = ON`); err != nil {
		return err
	}
	var version int64
	err = conn.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&version)
	if err == nil {
		_, err = conn.ExecContext(ctx, `DELETE FROM sqlite_schema WHERE type = 'index' AND sql IS NOT NULL`)
	}
	if err == nil {
		// Every connection reloads the schema, now without the old indexes.
		_, err = conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA schema_version = %d`, version+1))
	}
	conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA writable_schema = OFF`)
	if err != nil {
		return err
	}
	// If VACUUM fails the indexes are still created again, so the database
	// keeps working (with the damaged pages left over) until a backup is
	// restored.
	vacuumErr := retry(`VACUUM`)
	for _, ix := range indexes {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), ix.sql); err != nil {
			return fmt.Errorf("%s: %w", ix.name, err)
		}
	}
	if vacuumErr != nil {
		return fmt.Errorf("VACUUM: %w", vacuumErr)
	}
	return nil
}

// IsCorrupt reports whether err is SQLite saying the database file is
// damaged (SQLITE_CORRUPT or SQLITE_NOTADB).
func IsCorrupt(err error) bool {
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		c := coded.Code() & 0xff
		return c == 11 || c == 26
	}
	return err != nil && strings.Contains(err.Error(), "database disk image is malformed")
}

// CheckIntegrity runs SQLite's quick check (the file's structure, without
// comparing every index to its table) and returns its first complaints, or
// "" when the database is sound.
func (s *Store) CheckIntegrity(ctx context.Context) (string, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	problem := checkProblem(ctx, conn, `PRAGMA quick_check(5)`)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return problem, nil
}

// integrityProblem runs SQLite's full integrity check and returns its first
// complaints, or "" when the database is sound.
func integrityProblem(ctx context.Context, conn *sql.Conn) string {
	return checkProblem(ctx, conn, `PRAGMA integrity_check(5)`)
}

func checkProblem(ctx context.Context, conn *sql.Conn, pragma string) string {
	rows, err := conn.QueryContext(ctx, pragma)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err.Error()
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return err.Error()
	}
	if len(lines) == 1 && lines[0] == "ok" {
		return ""
	}
	if len(lines) == 0 {
		return "no result"
	}
	return strings.Join(lines, "; ")
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
