package livetv

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"bingebandit/internal/store"
)

func TestBuildPlayout(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	movies := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	cartoons := &store.Library{Name: "Cartoons", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(movies)
	st.CreateLibrary(cartoons)
	add := func(lib *store.Library, title string, minutes int) {
		id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, MatchKey: "m:" + title})
		st.UpsertFile(&store.MediaFile{ItemID: id, Path: "/x/" + title + ".mkv", Duration: float64(minutes * 60), ProbedAt: 1}, 1)
	}
	for i := 0; i < 5; i++ {
		add(movies, fmt.Sprintf("Movie %d", i), 90+i)
	}
	add(cartoons, "Cartoon A", 30)
	add(cartoons, "Cartoon B", 30)

	ch := &store.Channel{Number: 100, Name: "Mix", Enabled: true}
	err = st.SaveCustomChannel(ch, []*store.Block{
		{StartMinute: -1, EndMinute: -1, Mode: "shuffle", Sources: []store.ContentSource{{Type: "library", ID: movies.ID}}},
		{StartMinute: 6 * 60, EndMinute: 9 * 60, Mode: "order", Sources: []store.ContentSource{{Type: "library", ID: cartoons.ID}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.Local)
	n, err := BuildPlayout(st, ch.ID, now)
	if err != nil || n == 0 {
		t.Fatalf("built %d entries: %v", n, err)
	}
	items, _ := st.PlayoutPrograms(ch.ID, now.Unix(), now.Add(48*time.Hour).Unix())

	// Contiguous, reaching the horizon.
	for i := 1; i < len(items); i++ {
		if items[i].Start != items[i-1].Stop {
			t.Fatalf("gap between %q and %q", items[i-1].Title, items[i].Title)
		}
	}
	if last := items[len(items)-1].Stop; last < now.Add(48*time.Hour).Unix() {
		t.Errorf("schedule ends at %v, before the 48h horizon", time.Unix(last, 0))
	}
	// Shuffle airs each movie once before any repeats.
	seen := map[string]bool{}
	for _, it := range items {
		if len(seen) == 5 {
			break
		}
		if seen[it.Title] && it.Title[:5] == "Movie" {
			t.Fatalf("%q repeated before all 5 movies aired", it.Title)
		}
		if it.Title[:5] == "Movie" {
			seen[it.Title] = true
		}
	}
	// Cartoons start in the 6–9 AM block, in order.
	var morning []string
	for _, it := range items {
		h := time.Unix(it.Start, 0).Hour()
		if h >= 6 && h < 9 && time.Unix(it.Start, 0).Day() == 24 {
			morning = append(morning, it.Title)
		}
	}
	if len(morning) < 2 || morning[0] != "Cartoon A" || morning[1] != "Cartoon B" {
		t.Errorf("morning block = %v, want Cartoon A, Cartoon B, …", morning)
	}

	// Refilling continues where the schedule ended instead of duplicating.
	n2, _ := BuildPlayout(st, ch.ID, now)
	if n2 != 0 {
		t.Errorf("rebuilding an already-full schedule added %d entries", n2)
	}
	if it, err := st.PlayoutAt(ch.ID, now.Add(time.Hour).Unix()); err != nil || it.FileID == 0 {
		t.Errorf("PlayoutAt = %+v, %v", it, err)
	}
}

func TestCancelAndRestartPlayout(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib := &store.Library{Name: "Shows", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	for i := 0; i < 4; i++ {
		title := fmt.Sprintf("Ep %d", i)
		id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, SortTitle: title, MatchKey: "m:" + title})
		st.UpsertFile(&store.MediaFile{ItemID: id, Path: "/x/" + title + ".mkv", Duration: 3600, ProbedAt: 1}, 1)
	}
	ch := &store.Channel{Number: 5, Name: "Eps", Enabled: true}
	if err := st.SaveCustomChannel(ch, []*store.Block{{StartMinute: -1, EndMinute: -1, Mode: "order", Sources: []store.ContentSource{{Type: "library", ID: lib.ID}}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.Local)
	if _, err := BuildPlayout(st, ch.ID, now); err != nil {
		t.Fatal(err)
	}
	day := func() []store.GuideProgram {
		p, _ := st.PlayoutPrograms(ch.ID, now.Unix(), now.Add(48*time.Hour).Unix())
		return p
	}
	before := day()

	// Cancel the second entry: the third moves into its slot.
	if airing, err := st.CancelPlayout(ch.ID, -before[1].ID, now.Unix()); err != nil || airing {
		t.Fatalf("cancel: airing=%v err=%v", airing, err)
	}
	after := day()
	if after[1].Title != before[2].Title || after[1].Start != before[1].Start {
		t.Errorf("slot 2 = %q at %d, want %q at %d", after[1].Title, after[1].Start, before[2].Title, before[1].Start)
	}

	// Cancel what airs now, 20 minutes in: the next entry starts right away.
	mid := now.Add(20 * time.Minute)
	if airing, err := st.CancelPlayout(ch.ID, -after[0].ID, mid.Unix()); err != nil || !airing {
		t.Fatalf("cancel airing: airing=%v err=%v", airing, err)
	}
	if it, err := st.PlayoutAt(ch.ID, mid.Unix()); err != nil || it.Title != after[1].Title {
		t.Errorf("airing after cancel = %v (%v), want %q", it, err, after[1].Title)
	}

	// Restart from mid: in order starts over at the first episode.
	if _, err := RestartPlayout(st, ch.ID, mid); err != nil {
		t.Fatal(err)
	}
	it, err := st.PlayoutAt(ch.ID, mid.Unix())
	if err != nil || it.Title != "Ep 0" || it.Start != mid.Unix() {
		t.Errorf("after restart airing %v (%v), want Ep 0 from now", it, err)
	}
	p := day()
	for i := 1; i < len(p); i++ {
		if p[i].Start != p[i-1].Stop {
			t.Fatalf("gap after restart between %q and %q", p[i-1].Title, p[i].Title)
		}
	}
}
