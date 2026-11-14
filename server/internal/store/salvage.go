package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// When a table itself is damaged (not just an index), rebuilding indexes
// can't help, and restoring a backup throws away everything since it was
// made. Salvage instead builds a fresh database from every row that still
// reads, fills the rows it couldn't read from the newest sound backup, and
// copies the result over the live file page by page.
//
// Page copies (SQLite's online backup) never read the b-trees, so they
// work on a damaged file: they're also how a damaged database is saved
// before anything replaces it, and how a restore replaces it (deleting
// rows from a damaged table fails).

// errTableDamaged is rebuildIndexes refusing because a table doesn't read.
type errTableDamaged struct {
	table string
	err   error
}

func (e *errTableDamaged) Error() string {
	return fmt.Sprintf("table %s is damaged too: %v", e.table, e.err)
}

func (e *errTableDamaged) Unwrap() error { return e.err }

type pageCopier interface {
	NewBackup(dstURI string) (*sqlite.Backup, error)
	NewRestore(srcURI string) (*sqlite.Backup, error)
}

// copyPages copies the live database to path as it is, damage included.
func copyPages(ctx context.Context, conn *sql.Conn, path string) error {
	os.Remove(path)
	return conn.Raw(func(dc any) error {
		pc, ok := dc.(pageCopier)
		if !ok {
			return errors.New("the SQLite driver can't copy pages")
		}
		b, err := pc.NewBackup(path)
		if err != nil {
			return err
		}
		return stepAll(ctx, b)
	})
}

// restorePages replaces the live database with the one at path, page by
// page. Other connections see the new contents on their next statement.
func restorePages(ctx context.Context, conn *sql.Conn, path string) error {
	return conn.Raw(func(dc any) error {
		pc, ok := dc.(pageCopier)
		if !ok {
			return errors.New("the SQLite driver can't copy pages")
		}
		b, err := pc.NewRestore(path)
		if err != nil {
			return err
		}
		return stepAll(ctx, b)
	})
}

func stepAll(ctx context.Context, b *sqlite.Backup) error {
	busy := 0
	for {
		more, err := b.Step(-1)
		if err != nil && IsBusy(err) && busy < 120 && ctx.Err() == nil {
			// Another connection is mid-write; it finishes in moments.
			busy++
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if err != nil {
			b.Finish()
			return err
		}
		if !more {
			return b.Finish()
		}
	}
}

// SaveDamagedCopy copies the live database to path as it is. A normal
// backup (VACUUM INTO) reads every row, so it fails on a damaged file;
// this keeps a copy before a restore or repair replaces it.
func (s *Store) SaveDamagedCopy(ctx context.Context, path string) error {
	s.heavy.Lock()
	defer s.heavy.Unlock()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	return copyPages(ctx, conn, path)
}

// mainPath is the file of the live database.
func mainPath(ctx context.Context, conn *sql.Conn) (string, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" {
			return file, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", errors.New("the database has no file")
}

type schemaEntry struct{ kind, name, table, sql string }

// salvage rebuilds the live database from the rows that still read, with
// lost rows filled from the first sound backup in fallbacks (newest
// first). It returns a summary for the task result.
func (s *Store) salvage(ctx context.Context, conn *sql.Conn, fallbacks []string) (string, error) {
	live, err := mainPath(ctx, conn)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(live)
	stamp := time.Now().Format("20060102-150405")

	// First a copy of the file as it is, in case anything below goes wrong.
	damaged := filepath.Join(dir, "damaged-"+stamp+".db")
	if err := copyPages(ctx, conn, damaged); err != nil {
		return "", fmt.Errorf("couldn't save a copy of the damaged database first: %w", err)
	}

	var schema []schemaEntry
	rows, err := conn.QueryContext(ctx, `SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return "", fmt.Errorf("the list of tables is damaged: %w", err)
	}
	for rows.Next() {
		var e schemaEntry
		if err := rows.Scan(&e.kind, &e.name, &e.table, &e.sql); err != nil {
			rows.Close()
			return "", err
		}
		schema = append(schema, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("the list of tables is damaged: %w", err)
	}
	var version, pageSize int64
	conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	conn.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize)

	// The new file gets the tables and indexes now (unique indexes keep
	// backup rows from clashing with salvaged ones), triggers at the end.
	out := filepath.Join(dir, "salvage-"+stamp+".db")
	os.Remove(out)
	defer os.Remove(out)
	defer os.Remove(out + "-journal")
	if err := createSalvageFile(out, schema, version, pageSize); err != nil {
		return "", err
	}

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return "", err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`)
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS rc`, out); err != nil {
		return "", err
	}
	attached := true
	// A scratch file, checked before it's used: no need to sync each row.
	conn.ExecContext(ctx, `PRAGMA rc.synchronous = OFF`)
	conn.ExecContext(ctx, `PRAGMA rc.journal_mode = MEMORY`)
	detach := func() {
		if attached {
			conn.ExecContext(context.WithoutCancel(ctx), `DETACH DATABASE rc`)
			attached = false
		}
	}
	defer detach()

	type loss struct {
		table      string
		saved, bad int
	}
	var damagedTables []loss
	for _, e := range schema {
		if e.kind != "table" {
			continue
		}
		cols, err := tableColumns(ctx, conn, "main", e.name)
		if err != nil {
			return "", fmt.Errorf("table %s: %w", e.name, err)
		}
		list := strings.Join(cols, ", ")
		q := quoteIdent(e.name)
		_, err = conn.ExecContext(ctx, `INSERT INTO rc.`+q+` (`+list+`) SELECT `+list+` FROM main.`+q+` NOT INDEXED`)
		if err == nil {
			continue
		}
		if !IsCorrupt(err) {
			return "", fmt.Errorf("copy %s: %w", e.name, err)
		}
		saved, bad := salvageRows(ctx, conn, e.name, list, schema)
		damagedTables = append(damagedTables, loss{e.name, saved, bad})
		slog.Warn("salvaged a damaged table", "table", e.name, "rows", saved, "unreadable", bad)
	}
	// AUTOINCREMENT counters never go backwards, or old ids would be reused.
	if seq, err := conn.QueryContext(ctx, `SELECT name, seq FROM main.sqlite_sequence`); err == nil {
		type counter struct {
			name string
			seq  int64
		}
		var cs []counter
		for seq.Next() {
			var c counter
			if seq.Scan(&c.name, &c.seq) == nil {
				cs = append(cs, c)
			}
		}
		seq.Close()
		for _, c := range cs {
			conn.ExecContext(ctx, `UPDATE rc.sqlite_sequence SET seq = max(seq, ?) WHERE name = ?`, c.seq, c.name)
			conn.ExecContext(ctx, `INSERT INTO rc.sqlite_sequence (name, seq) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM rc.sqlite_sequence WHERE name = ?)`, c.name, c.seq, c.name)
		}
	}

	// Rows that didn't read come back from the newest sound backup.
	fromBackup := map[string]int{}
	backupName := ""
	if len(damagedTables) > 0 {
		fb, name, cleanup := soundBackup(fallbacks)
		defer cleanup()
		if fb != "" {
			backupName = name
			if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS fb`, fb); err == nil {
				for _, d := range damagedTables {
					n, err := fillFromBackup(ctx, conn, d.table)
					if err != nil {
						slog.Warn("couldn't fill rows from the backup", "table", d.table, "err", err)
					}
					fromBackup[d.table] = n
				}
				conn.ExecContext(context.WithoutCancel(ctx), `DETACH DATABASE fb`)
			}
		}
	}

	// Rows pointing at rows that were lost can't stay (the server expects
	// every reference to resolve); deleting them can orphan more, so repeat.
	orphans := 0
	for pass := 0; pass < 10; pass++ {
		n, err := deleteDangling(ctx, conn)
		if err != nil {
			return "", err
		}
		if n == 0 {
			break
		}
		orphans += n
	}

	for _, e := range schema {
		if e.kind == "trigger" || e.kind == "view" {
			if _, err := conn.ExecContext(ctx, strings.Replace(e.sql, e.name, "rc."+quoteIdent(e.name), 1)); err != nil {
				return "", fmt.Errorf("recreate %s: %w", e.name, err)
			}
		}
	}
	if problem := checkProblem(ctx, conn, `PRAGMA rc.integrity_check(5)`); problem != "" {
		return "", fmt.Errorf("the rebuilt database didn't check out (%s); the damaged one is unchanged", problem)
	}
	detach()

	if err := restorePages(ctx, conn, out); err != nil {
		return "", fmt.Errorf("couldn't put the rebuilt database in place (%v); the damaged one is unchanged", err)
	}

	var parts []string
	for _, d := range damagedTables {
		p := fmt.Sprintf("%s: %d rows read", d.table, d.saved)
		if n := fromBackup[d.table]; n > 0 {
			p += fmt.Sprintf(", %d came back from %s", n, backupName)
		}
		parts = append(parts, p)
	}
	result := "rebuilt the damaged database from what still read (" + strings.Join(parts, "; ") + ")"
	if len(damagedTables) > 0 && backupName == "" {
		result += "; no sound backup was found to fill in the rest"
	}
	if orphans > 0 {
		result += fmt.Sprintf("; %d rows that pointed at lost ones were removed", orphans)
	}
	result += "; the damaged file is kept as " + filepath.Base(damaged) + ". Run Scan libraries to pick up anything still missing"
	return result, nil
}

func createSalvageFile(path string, schema []schemaEntry, version, pageSize int64) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if pageSize > 0 {
		if _, err := db.Exec(fmt.Sprintf(`PRAGMA page_size = %d`, pageSize)); err != nil {
			return err
		}
	}
	for _, kind := range []string{"table", "index"} {
		for _, e := range schema {
			if e.kind == kind {
				if _, err := db.Exec(e.sql); err != nil {
					return fmt.Errorf("create %s: %w", e.name, err)
				}
			}
		}
	}
	_, err = db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version))
	return err
}

// tableColumns lists the quoted stored columns of table in schema.
func tableColumns(ctx context.Context, conn *sql.Conn, schema, table string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, `SELECT name, hidden FROM pragma_table_xinfo(?, ?)`, table, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		var hidden int
		if err := rows.Scan(&name, &hidden); err != nil {
			return nil, err
		}
		if hidden == 0 {
			out = append(out, quoteIdent(name))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no columns")
	}
	return out, rows.Err()
}

// salvageRows copies the rows of a damaged table that still read, one at
// a time. A scan stops at the first damaged page, so the row ids come from
// scanning up to it from both ends and from the table's indexes (which
// hold the ids of rows on the damaged pages too); each row is then looked
// up by id, which only fails for rows on a damaged page.
func salvageRows(ctx context.Context, conn *sql.Conn, table, cols string, schema []schemaEntry) (saved, bad int) {
	q := quoteIdent(table)
	ids := map[int64]bool{}
	collect := func(query string) {
		rows, err := conn.QueryContext(ctx, query)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if rows.Scan(&id) != nil {
				return
			}
			ids[id] = true
		}
	}
	collect(`SELECT rowid FROM main.` + q + ` NOT INDEXED ORDER BY rowid`)
	collect(`SELECT rowid FROM main.` + q + ` NOT INDEXED ORDER BY rowid DESC`)
	for _, e := range schema {
		if e.kind == "index" && e.table == table {
			collect(`SELECT rowid FROM main.` + q + ` INDEXED BY ` + quoteIdent(e.name))
		}
	}
	sorted := make([]int64, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	insert := `INSERT OR IGNORE INTO rc.` + q + ` (` + cols + `) SELECT ` + cols + ` FROM main.` + q + ` NOT INDEXED WHERE rowid = ?`
	// Each row in its own statement and transaction: SQLite rolls a whole
	// transaction back when it meets a damaged page.
	for _, id := range sorted {
		if ctx.Err() != nil {
			break
		}
		if res, err := conn.ExecContext(ctx, insert, id); err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				saved++
				continue
			}
		}
		bad++
	}
	return saved, bad
}

// soundBackup returns a migrated copy of the first backup in paths that
// passes the integrity check, its name, and a function removing the copy.
func soundBackup(paths []string) (string, string, func()) {
	for _, p := range paths {
		tmp, err := os.CreateTemp(filepath.Dir(p), "salvage-backup-*.db")
		if err != nil {
			continue
		}
		tmpPath := tmp.Name()
		tmp.Close()
		cleanup := func() {
			for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
				os.Remove(tmpPath + suffix)
			}
		}
		if copyFile(p, tmpPath) == nil && checkBackup(tmpPath) == nil {
			return tmpPath, filepath.Base(p), cleanup
		}
		cleanup()
	}
	return "", "", func() {}
}

// fillFromBackup adds the backup's rows of table whose ids the salvage
// didn't find. Rows deleted since the backup can come back; a scan removes
// those whose files are gone.
func fillFromBackup(ctx context.Context, conn *sql.Conn, table string) (int, error) {
	main, err := tableColumns(ctx, conn, "rc", table)
	if err != nil {
		return 0, err
	}
	backup, err := tableColumns(ctx, conn, "fb", table)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, c := range backup {
		have[c] = true
	}
	var shared []string
	for _, c := range main {
		if have[c] {
			shared = append(shared, c)
		}
	}
	list := strings.Join(shared, ", ")
	q := quoteIdent(table)
	res, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO rc.`+q+` (`+list+`) SELECT `+list+` FROM fb.`+q+
		` WHERE rowid NOT IN (SELECT rowid FROM rc.`+q+`)`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// deleteDangling removes the rows of the rebuilt database whose foreign keys
// point at missing rows, and returns how many.
func deleteDangling(ctx context.Context, conn *sql.Conn) (int, error) {
	rows, err := conn.QueryContext(ctx, `PRAGMA rc.foreign_key_check`)
	if err != nil {
		return 0, err
	}
	type orphan struct {
		table string
		id    sql.NullInt64
	}
	var list []orphan
	for rows.Next() {
		var o orphan
		var parent string
		var fkid int
		if err := rows.Scan(&o.table, &o.id, &parent, &fkid); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, o := range list {
		if !o.id.Valid {
			continue // WITHOUT ROWID: none in this schema
		}
		if res, err := conn.ExecContext(ctx, `DELETE FROM rc.`+quoteIdent(o.table)+` WHERE rowid = ?`, o.id.Int64); err == nil {
			if k, _ := res.RowsAffected(); k > 0 {
				n++
			}
		}
	}
	return n, nil
}
