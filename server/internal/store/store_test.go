package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addMovie(t *testing.T, s *Store, lib int64, title string) int64 {
	t.Helper()
	id, err := s.UpsertItem(&Item{LibraryID: lib, Kind: "movie", Title: title, MatchKey: "movie:" + title})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLockedFieldsSurviveRefresh(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	if err := s.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	id := addMovie(t, s, lib.ID, "Alien")

	if err := s.UpdateDetails(id, &Details{Title: "Alien (Director's Cut)", Overview: "mine", Locked: []string{"title", "bogus"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMetadata(id, &Item{Title: "Alien", Overview: "from tmdb", Poster: "tmdb:/a.jpg"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetArtwork(id, "backdrop", "local:/0123456789abcdef0123456789abcdef.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMetadata(id, &Item{Title: "Alien", Overview: "again", Backdrop: "tmdb:/b.jpg"}); err != nil {
		t.Fatal(err)
	}

	it, err := s.Item(0, id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Alien (Director's Cut)" || it.SortTitle != "Alien (Director's Cut)" {
		t.Errorf("locked title overwritten: %q / %q", it.Title, it.SortTitle)
	}
	if it.Overview != "again" {
		t.Errorf("unlocked overview not refreshed: %q", it.Overview)
	}
	if it.Backdrop != "/api/images/local/0123456789abcdef0123456789abcdef.jpg" {
		t.Errorf("locked backdrop overwritten: %q", it.Backdrop)
	}
	want := map[string]bool{"title": true, "backdrop": true}
	if len(it.Locked) != 2 || !want[it.Locked[0]] || !want[it.Locked[1]] {
		t.Errorf("locked = %v, want title+backdrop (unknown fields dropped)", it.Locked)
	}
}

func TestLibraryVisibility(t *testing.T) {
	s := openTest(t)
	kids := &Library{Name: "Kids", Kind: "movies", Paths: []string{}}
	adults := &Library{Name: "Adults", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(kids)
	s.CreateLibrary(adults)
	kidMovie := addMovie(t, s, kids.ID, "Bluey")
	adultMovie := addMovie(t, s, adults.ID, "Heat")

	u, _ := s.CreateUser("child", "x", false)
	if err := s.SetUserLibraries(u.ID, false, []int64{kids.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Item(u.ID, kidMovie); err != nil {
		t.Errorf("child should see Kids: %v", err)
	}
	if _, err := s.Item(u.ID, adultMovie); !errors.Is(err, ErrNotFound) {
		t.Errorf("child should not see Adults, got %v", err)
	}
	recent, _ := s.RecentlyAdded(u.ID, "movie", 10)
	if len(recent) != 1 || recent[0].ID != kidMovie {
		t.Errorf("recently added leaks other libraries: %d items", len(recent))
	}
	u, _ = s.UserByID(u.ID)
	if s.CanSeeLibrary(u, adults.ID) || !s.CanSeeLibrary(u, kids.ID) {
		t.Error("CanSeeLibrary disagrees with user_libraries")
	}
	if _, err := s.Item(0, adultMovie); err != nil {
		t.Errorf("server (uid 0) must see everything: %v", err)
	}
}

func TestPruneUnderAndMaintenance(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "TV", Kind: "shows", Paths: []string{}}
	s.CreateLibrary(lib)
	sep := string(filepath.Separator)
	a := addMovie(t, s, lib.ID, "a")
	b := addMovie(t, s, lib.ID, "b")
	s.UpsertFile(&MediaFile{ItemID: a, Path: sep + "media" + sep + "Show" + sep + "e1.mkv"}, 1)
	s.UpsertFile(&MediaFile{ItemID: b, Path: sep + "media" + sep + "Show 2" + sep + "e1.mkv"}, 1)

	// Rescanning "Show" at generation 2 found nothing: only its file goes,
	// not the one in "Show 2" which merely shares the name prefix.
	removed, err := s.PruneUnder(lib.ID, 2, []string{sep + "media" + sep + "Show" + sep})
	if err != nil || removed != 1 {
		t.Fatalf("PruneUnder removed %d, err %v; want 1", removed, err)
	}
	if _, err := s.Item(0, a); !errors.Is(err, ErrNotFound) {
		t.Error("item without files should be deleted")
	}
	if _, err := s.Item(0, b); err != nil {
		t.Error("item outside the pruned folder was deleted")
	}

	backup := filepath.Join(t.TempDir(), "backup.db")
	if err := s.Backup(backup); err != nil {
		t.Fatalf("backup: %v", err)
	}
	copyDB, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copyDB.Item(0, b); err != nil {
		t.Errorf("backup is missing data: %v", err)
	}
	copyDB.Close()
	if err := s.Optimize(); err != nil {
		t.Errorf("optimize: %v", err)
	}

	if err := s.SetTaskConfig("x", true, 12); err != nil {
		t.Fatal(err)
	}
	st, ok, _ := s.TaskState("x")
	if !ok || !st.Enabled || st.IntervalHours != 12 {
		t.Errorf("task state = %+v, %v", st, ok)
	}
}

func TestCollections(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	a := addMovie(t, s, lib.ID, "The Lion King")
	b := addMovie(t, s, lib.ID, "The Lion King II")
	c := addMovie(t, s, lib.ID, "Moana")

	if err := s.LinkTMDBCollection(a, lib.ID, 94032, "The Lion King Collection", "tmdb:/p.jpg", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkTMDBCollection(b, lib.ID, 94032, "The Lion King Collection", "", ""); err != nil {
		t.Fatal(err)
	}
	// A smart collection driven by a label.
	smart, err := s.CreateCollection(lib.ID, "Disney Animation", "disney")
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateDetails(c, &Details{Title: "Moana", Labels: []string{"Disney"}})
	s.UpdateDetails(a, &Details{Title: "The Lion King", Labels: []string{"disney"}})

	cols, _ := s.LibraryCollections(lib.ID)
	counts := map[string]int{}
	for _, col := range cols {
		counts[col.Name] = col.ItemCount
	}
	if counts["The Lion King Collection"] != 2 || counts["Disney Animation"] != 2 {
		t.Errorf("collection counts = %v", counts)
	}
	members, _ := s.CollectionMembers(0, smart.ID)
	if len(members) != 2 {
		t.Errorf("smart collection members = %d, want 2", len(members))
	}
	in, _ := s.ItemCollections(a)
	if len(in) != 2 {
		t.Errorf("Lion King should be in 2 collections, got %d", len(in))
	}

	// Locking collections stops TMDB from re-adding the link.
	s.UpdateDetails(b, &Details{Title: "The Lion King II", Locked: []string{"collections"}})
	s.SetItemCollections(b, lib.ID, nil)
	s.LinkTMDBCollection(b, lib.ID, 94032, "The Lion King Collection", "", "")
	if names, _ := s.ItemCollectionNames(b); len(names) != 0 {
		t.Errorf("locked collections were re-linked: %v", names)
	}
}
