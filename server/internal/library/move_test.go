package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func writeFile(t *testing.T, p string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A show moved to Kids TV and back keeps its id and watch state, and later
// scans of both libraries leave it be.
func TestMoveShowBetweenLibraries(t *testing.T) {
	dir := t.TempDir()
	tv, kids := filepath.Join(dir, "TV"), filepath.Join(dir, "Kids TV")
	writeFile(t, filepath.Join(tv, "Bluey (2018)", "Season 1", "Bluey - S01E01.mkv"))
	writeFile(t, filepath.Join(tv, "Bluey (2018)", "Season 1", "Bluey - S01E02.mkv"))
	writeFile(t, filepath.Join(tv, "The Wire", "Season 1", "The Wire - S01E01.mkv"))
	os.MkdirAll(kids, 0o755)

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	tvLib := &store.Library{Name: "TV Shows", Kind: "shows", Paths: []string{tv}}
	kidsLib := &store.Library{Name: "Kids TV Shows", Kind: "shows", Paths: []string{kids}}
	for _, l := range []*store.Library{tvLib, kidsLib} {
		if err := st.CreateLibrary(l); err != nil {
			t.Fatal(err)
		}
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	scan := func() {
		t.Helper()
		for _, l := range []*store.Library{tvLib, kidsLib} {
			if err := sc.scanFiles(context.Background(), l, &Status{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	titles := func(l *store.Library) map[string]*store.Item {
		t.Helper()
		items, _, err := st.LibraryItems(u.ID, l.ID, "title", 0, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*store.Item{}
		for _, it := range items {
			out[it.Title] = it
		}
		return out
	}
	scan()
	bluey := titles(tvLib)["Bluey"]
	if bluey == nil {
		t.Fatal("no Bluey after the scan")
	}
	eps, _ := st.ShowEpisodes(u.ID, bluey.ID)
	if err := st.SetWatched(u.ID, []int64{eps[0].ID}, true); err != nil {
		t.Fatal(err)
	}

	loc, err := sc.Locate(bluey)
	if err != nil || loc.Problem != "" || len(loc.Paths) != 1 || loc.Paths[0] != filepath.Join(tv, "Bluey (2018)") {
		t.Fatalf("Locate = %+v, %v", loc, err)
	}
	targets, _ := sc.MoveTargets(loc)
	if len(targets) != 1 || targets[0].LibraryID != kidsLib.ID {
		t.Fatalf("targets = %+v", targets)
	}

	if copying, err := sc.Move(bluey, kidsLib.ID, kids); err != nil || copying {
		t.Fatalf("Move = %v, %v", copying, err)
	}
	if !isDir(filepath.Join(kids, "Bluey (2018)", "Season 1")) || isDir(filepath.Join(tv, "Bluey (2018)")) {
		t.Fatal("the folder didn't move")
	}
	check := func(l *store.Library) {
		t.Helper()
		got := titles(l)["Bluey"]
		if got == nil || got.ID != bluey.ID {
			t.Fatalf("Bluey in %s = %+v, want item %d", l.Name, got, bluey.ID)
		}
		eps, _ := st.ShowEpisodes(u.ID, got.ID)
		if len(eps) != 2 || !eps[0].Watch.Watched {
			t.Fatalf("episodes = %d, first watched %v", len(eps), len(eps) > 0 && eps[0].Watch.Watched)
		}
		fs, _ := st.FilesForItem(eps[0].ID)
		if len(fs) != 1 || rootFor(l, fs[0].Path) == "" {
			t.Fatalf("file = %+v", fs)
		}
	}
	check(kidsLib)
	if titles(tvLib)["Bluey"] != nil {
		t.Fatal("Bluey is still in TV Shows")
	}
	scan()
	check(kidsLib)
	if len(titles(tvLib)) != 1 {
		t.Fatalf("TV Shows = %v", titles(tvLib))
	}

	// And back.
	if _, err := sc.Move(bluey, tvLib.ID, tv); err != nil {
		t.Fatal(err)
	}
	scan()
	check(tvLib)
	if len(titles(kidsLib)) != 0 {
		t.Fatalf("Kids TV Shows = %v", titles(kidsLib))
	}

	// A show of the same name already there isn't overwritten.
	writeFile(t, filepath.Join(kids, "Bluey (2018)", "Season 2", "Bluey - S02E01.mkv"))
	if _, err := sc.Move(bluey, kidsLib.ID, kids); err == nil {
		t.Fatal("moved onto an existing folder")
	}
}

// A movie loose in a folder with other movies moves with its subtitles,
// leaving the others behind.
func TestMoveLooseMovie(t *testing.T) {
	dir := t.TempDir()
	movies, kids := filepath.Join(dir, "Movies"), filepath.Join(dir, "Kids Movies")
	writeFile(t, filepath.Join(movies, "Cars (2006).mkv"))
	writeFile(t, filepath.Join(movies, "Cars (2006).en.srt"))
	writeFile(t, filepath.Join(movies, "Heat (1995).mkv"))
	os.MkdirAll(kids, 0o755)

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	from := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{movies}}
	to := &store.Library{Name: "Kids Movies", Kind: "movies", Paths: []string{kids}}
	for _, l := range []*store.Library{from, to} {
		if err := st.CreateLibrary(l); err != nil {
			t.Fatal(err)
		}
	}
	u, _ := st.CreateUser("admin", "", true)
	sc := NewScanner(st, nil, "ffprobe-missing")
	if err := sc.scanFiles(context.Background(), from, &Status{}); err != nil {
		t.Fatal(err)
	}
	items, _, _ := st.LibraryItems(u.ID, from.ID, "title", 0, 10, nil)
	var cars *store.Item
	for _, it := range items {
		if it.Title == "Cars" {
			cars = it
		}
	}
	if cars == nil {
		t.Fatalf("items = %v", items)
	}
	if err := st.SetItemCollections(cars.ID, from.ID, []string{"Pixar"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Move(cars, to.ID, kids); err != nil {
		t.Fatal(err)
	}
	if names, _ := st.ItemCollectionNames(cars.ID); len(names) != 1 || names[0] != "Pixar" {
		t.Errorf("collections after the move = %v", names)
	}
	if cols, _ := st.LibraryCollections(to.ID); len(cols) != 1 || cols[0].Name != "Pixar" {
		t.Errorf("Kids Movies collections = %v", cols)
	}
	for _, p := range []string{filepath.Join(kids, "Cars (2006).mkv"), filepath.Join(kids, "Cars (2006).en.srt"), filepath.Join(movies, "Heat (1995).mkv")} {
		if _, err := os.Stat(p); err != nil {
			t.Error(err)
		}
	}
	if err := sc.scanFiles(context.Background(), to, &Status{}); err != nil {
		t.Fatal(err)
	}
	moved, _, _ := st.LibraryItems(u.ID, to.ID, "title", 0, 10, nil)
	if len(moved) != 1 || moved[0].ID != cars.ID {
		t.Fatalf("Kids Movies = %v", moved)
	}
}

// Between drives the files are copied under a hidden name, then put in place.
func TestCopySteps(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a", "Show")
	writeFile(t, filepath.Join(src, "Season 1", "Show - S01E01.mkv"))
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	os.Chtimes(filepath.Join(src, "Season 1", "Show - S01E01.mkv"), old, old)
	dst := filepath.Join(dir, "b", "Show")
	os.MkdirAll(filepath.Dir(dst), 0o755)
	sc := &Scanner{status: map[int64]*Status{}}
	if err := sc.copySteps([]moveStep{{src, dst}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "Season 1", "Show - S01E01.mkv"))
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("copy = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b", ".bingebandit-moving-Show")); !os.IsNotExist(err) {
		t.Error("the hidden copy is still there")
	}
}

func TestHiddenBelow(t *testing.T) {
	root := filepath.Join("/media", ".hidden", "TV")
	if hiddenBelow(root, filepath.Join(root, "Show", "Season 1")) {
		t.Error("a library inside a dot folder isn't hidden")
	}
	if !hiddenBelow(root, filepath.Join(root, ".bingebandit-moving-Show", "Season 1")) {
		t.Error("a dot folder in the library is hidden")
	}
}

// A folder's match key follows the folders around it: "Clubhouse+" keeps its
// own key next to "Clubhouse", and the plain one where it's alone.
func TestMoveShowKeyFollowsFolders(t *testing.T) {
	dir := t.TempDir()
	tv, kids := filepath.Join(dir, "TV"), filepath.Join(dir, "Kids")
	writeFile(t, filepath.Join(tv, "Mickey Mouse Clubhouse", "Season 1", "Mickey Mouse Clubhouse - S01E01.mkv"))
	writeFile(t, filepath.Join(tv, "Mickey Mouse Clubhouse+", "Season 1", "Mickey Mouse Clubhouse+ - S01E01.mkv"))
	os.MkdirAll(kids, 0o755)
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &store.Library{Name: "TV", Kind: "shows", Paths: []string{tv}}
	b := &store.Library{Name: "Kids", Kind: "shows", Paths: []string{kids}}
	st.CreateLibrary(a)
	st.CreateLibrary(b)
	u, _ := st.CreateUser("admin", "", true)
	sc := NewScanner(st, nil, "ffprobe-missing")
	if err := sc.scanFiles(context.Background(), a, &Status{}); err != nil {
		t.Fatal(err)
	}
	items, _, _ := st.LibraryItems(u.ID, a.ID, "title", 0, 10, nil)
	var plus *store.Item
	for _, it := range items {
		if it.Title == "Mickey Mouse Clubhouse+" {
			plus = it
		}
	}
	if plus == nil {
		t.Fatalf("items = %v", items)
	}
	for _, step := range []struct {
		lib  *store.Library
		root string
	}{{b, kids}, {a, tv}} {
		if _, err := sc.Move(plus, step.lib.ID, step.root); err != nil {
			t.Fatal(err)
		}
		for _, l := range []*store.Library{a, b} {
			if err := sc.scanFiles(context.Background(), l, &Status{}); err != nil {
				t.Fatal(err)
			}
		}
		got, _, _ := st.LibraryItems(u.ID, step.lib.ID, "title", 0, 10, nil)
		found := false
		for _, it := range got {
			found = found || it.ID == plus.ID
		}
		eps, _ := st.ShowEpisodes(u.ID, plus.ID)
		if !found || len(eps) != 1 {
			t.Fatalf("in %s: %v, episodes %d", step.lib.Name, got, len(eps))
		}
	}
}
