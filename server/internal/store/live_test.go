package store

import (
	"testing"
	"time"
)

func importStreams(t *testing.T, s *Store, src int64, entries ...StreamEntry) int {
	t.Helper()
	im, err := s.BeginStreamImport(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := im.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	n, err := im.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSourceGroupFilter(t *testing.T) {
	s := openTest(t)
	src := &LiveSource{Kind: "m3u", Name: "IPTV", URL: "http://x", RefreshHours: 24, Enabled: true}
	if err := s.SaveLiveSource(src); err != nil {
		t.Fatal(err)
	}
	playlist := []StreamEntry{
		{Name: "ESPN", URL: "http://x/1", Group: "US"},
		{Name: "ESPN Brazil", URL: "http://x/2", Group: "Brazil"},
		{Name: "ESPN 2", URL: "http://x/3", Group: "Brazil"},
	}
	if n := importStreams(t, s, src.ID, playlist...); n != 3 {
		t.Fatalf("first import = %d streams, want 3", n)
	}

	// A channel uses one Brazil stream; turning Brazil off keeps only that one.
	brazil, _, _ := s.LiveStreams(StreamQuery{Group: "Brazil", Search: "ESPN Brazil", Limit: 10})
	if _, err := s.CreateChannelsFromStreams([]int64{brazil[0].ID}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSourceGroups(src.ID, false, map[string]bool{"Brazil": false}); err != nil {
		t.Fatal(err)
	}
	playlist = append(playlist, StreamEntry{Name: "Sky Sports", URL: "http://x/4", Group: "UK"})
	if n := importStreams(t, s, src.ID, playlist...); n != 2 {
		t.Fatalf("filtered import = %d streams, want 2 (US + the Brazil stream in use)", n)
	}
	if _, total, _ := s.LiveStreams(StreamQuery{Group: "Brazil", Limit: 10}); total != 1 {
		t.Fatalf("Brazil streams = %d, want 1", total)
	}

	groups, err := s.SourceGroups(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]SourceGroup{
		"Brazil": {Name: "Brazil", Enabled: false, Streams: 2},
		"UK":     {Name: "UK", Enabled: false, Streams: 1}, // new groups off
		"US":     {Name: "US", Enabled: true, Streams: 1},
	}
	if len(groups) != len(want) {
		t.Fatalf("groups = %+v", groups)
	}
	for _, g := range groups {
		if want[g.Name] != g {
			t.Errorf("group %q = %+v, want %+v", g.Name, g, want[g.Name])
		}
	}
	got, _ := s.LiveSource(src.ID)
	if got.NewGroupsEnabled {
		t.Error("newGroupsEnabled should be false")
	}
}

// A refresh's import used to read in a deferred transaction and write later;
// if anything else wrote in between, its first insert failed at once with
// "database is locked (517)" whatever the busy timeout.
func TestStreamImportWithConcurrentWrite(t *testing.T) {
	s := openTest(t)
	src := &LiveSource{Kind: "m3u", Name: "IPTV", URL: "http://x", RefreshHours: 24, Enabled: true}
	if err := s.SaveLiveSource(src); err != nil {
		t.Fatal(err)
	}
	im, err := s.BeginStreamImport(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.SetSourceRefreshed(src.ID, 0, nil) }() // waits for the import
	time.Sleep(200 * time.Millisecond)
	if err := im.Add(StreamEntry{Name: "ESPN", URL: "http://x/1", Group: "US"}); err != nil {
		t.Fatalf("add while another write waits: %v", err)
	}
	if n, err := im.Finish(); err != nil || n != 1 {
		t.Fatalf("finish = %d, %v", n, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the other write: %v", err)
	}
}
