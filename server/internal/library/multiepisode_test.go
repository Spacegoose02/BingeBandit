package library

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bingebandit/internal/store"
)

func TestMultiEpisodeFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "TV")
	season := filepath.Join(root, "Bandit Show", "Season 1")
	os.MkdirAll(season, 0o755)
	write := func(name string) string {
		p := filepath.Join(season, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	double := write("Bandit Show - S01E01E02 - Pilot.mkv")
	write("Bandit Show - S01E03 - Third.mkv")

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "TV", Kind: "shows", Paths: []string{root}}
	if err := st.CreateLibrary(lib); err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser("admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	sc := NewScanner(st, nil, "ffprobe-missing")
	scan := func() map[int]*store.Item {
		t.Helper()
		if err := sc.scanFiles(context.Background(), lib, &Status{}); err != nil {
			t.Fatal(err)
		}
		shows, _, _ := st.LibraryItems(u.ID, lib.ID, "title", 0, 10, nil)
		if len(shows) != 1 {
			t.Fatalf("shows = %d", len(shows))
		}
		eps, err := st.ShowEpisodes(u.ID, shows[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int]*store.Item{}
		for _, e := range eps {
			out[*e.EpisodeNumber] = e
		}
		return out
	}

	eps := scan()
	if len(eps) != 3 || eps[1] == nil || eps[2] == nil || eps[3] == nil {
		t.Fatalf("episodes = %v; want 1, 2 and 3", eps)
	}
	if eps[1].Title != "Pilot" || eps[2].Title != "Episode 2" {
		t.Errorf("titles = %q, %q", eps[1].Title, eps[2].Title)
	}
	f1, _ := st.FilesForItem(eps[1].ID)
	f2, _ := st.FilesForItem(eps[2].ID)
	if len(f1) != 1 || len(f2) != 1 || f1[0].ID != f2[0].ID || f2[0].Path != double {
		t.Fatalf("episode 2 should play the same file as episode 1: %v, %v", f1, f2)
	}

	// Up next after episode 1 skips episode 2, which is in the same file.
	next, err := st.NextEpisode(u.ID, eps[1].ShowID, 1, 1)
	if err != nil || *next.EpisodeNumber != 3 {
		t.Fatalf("next after 1 = %v, %v; want episode 3", next, err)
	}
	// Finishing episode 1 finishes episode 2 too.
	if _, err := st.SaveProgress(u.ID, eps[1].ID, 95, 100); err != nil {
		t.Fatal(err)
	}
	if it, _ := st.Item(u.ID, eps[2].ID); it == nil || !it.Watch.Watched {
		t.Errorf("episode 2 not marked watched with episode 1")
	}

	// Renamed to a single episode: episode 2 goes away.
	os.Rename(double, filepath.Join(season, "Bandit Show - S01E01 - Pilot.mkv"))
	eps = scan()
	if len(eps) != 2 || eps[2] != nil {
		t.Fatalf("after rename, episodes = %v; want 1 and 3", eps)
	}
}
