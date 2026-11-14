package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A damaged index made every season of every show read as missing: the
// season count failed and was taken as "none here". Now the count reports
// the error, and Optimize database rebuilds the indexes.
func TestDamagedIndexIsReportedAndRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bb.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	lib := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 40; n++ {
		key := fmt.Sprintf("show:%d", n)
		show, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "show", Title: key, MatchKey: key})
		s.db.Exec(`UPDATE items SET tmdb_id = ? WHERE id = ?`, 1000+n, show)
		for se := 1; se <= 3; se++ {
			sn := se
			season, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "season", ParentID: show, ShowID: show, SeasonNumber: &sn, MatchKey: fmt.Sprintf("%s:s%d", key, se)})
			for e := 1; e <= 10; e++ {
				en := e
				s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "episode", ParentID: season, ShowID: show, SeasonNumber: &sn, EpisodeNumber: &en, MatchKey: fmt.Sprintf("%s:s%d:e%d", key, se, e)})
			}
		}
	}
	if have, err := s.ShowSeasons(1005); err != nil || have[1] != 10 || have[3] != 10 {
		t.Fatalf("before: %v %v", have, err)
	}

	// With statistics (Optimize database keeps them) the count reads an
	// index, as on a real server. Scribble over the items indexes (not the table) the season count can read.
	s.db.Exec(`ANALYZE`)
	var roots []int64
	var pageSize int64
	rows, _ := s.db.Query(`SELECT rootpage FROM sqlite_master WHERE type = 'index' AND tbl_name = 'items' AND name NOT LIKE 'sqlite_autoindex%'`)
	for rows.Next() {
		var r int64
		rows.Scan(&r)
		roots = append(roots, r)
	}
	rows.Close()
	s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
	s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	s.Close()
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	junk := make([]byte, pageSize)
	for i := range junk {
		junk[i] = 0xA5
	}
	for _, root := range roots {
		f.WriteAt(junk, (root-1)*pageSize)
	}
	f.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if have, err := s.ShowSeasons(1005); err == nil {
		t.Fatalf("damaged: got %v and no error; it would read as nothing here", have)
	}
	if covered := s.RequestCovered(&TitleRequest{Kind: "show", TMDBID: 1005}); covered {
		t.Fatal("a request counted as arrived with the counts unreadable")
	}

	if problem, err := s.CheckIntegrity(context.Background()); err != nil || problem == "" {
		t.Fatalf("quick check: %q %v", problem, err)
	}

	result, err := s.Optimize(context.Background())
	if err != nil {
		t.Fatalf("optimize: %v", err)
	}
	if !strings.Contains(result, "repaired") {
		t.Fatalf("result %q doesn't say it repaired anything", result)
	}
	if have, err := s.ShowSeasons(1005); err != nil || have[1] != 10 || have[2] != 10 || have[3] != 10 {
		t.Fatalf("after repair: %v %v", have, err)
	}
	if problem, err := s.CheckIntegrity(context.Background()); err != nil || problem != "" {
		t.Fatalf("quick check after repair: %q %v", problem, err)
	}
	// A sound database is left alone.
	if result, err := s.Optimize(context.Background()); err != nil || strings.Contains(result, "repaired") {
		t.Fatalf("second run: %q %v", result, err)
	}
}
