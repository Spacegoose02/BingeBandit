package store

import (
	"path/filepath"
	"testing"
)

func TestRecordingAdBreaks(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("dana", "x", true)
	lib := &Library{Name: "Recordings", Kind: "shows", Paths: []string{}}
	s.CreateLibrary(lib)
	ep, _ := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "movie", Title: "The News", MatchKey: "movie:news"})
	path := "/rec/The News.ts"
	if err := s.UpsertFile(&MediaFile{ItemID: ep, Path: path, Duration: 1800}, 1); err != nil {
		t.Fatal(err)
	}
	res, err := s.db.Exec(`INSERT INTO recordings (user_id, channel_name, title, program_start, program_stop, start_at, stop_at, status, path, created_at)
		VALUES (?, 'BBC', 'The News', 1, 2, 1, 2, 'completed', ?, 1)`, u.ID, path)
	if err != nil {
		t.Fatal(err)
	}
	recID, _ := res.LastInsertId()
	r := &Recording{ID: recID}

	todo, err := s.RecordingsNeedingCommercials(10)
	if err != nil || len(todo) != 1 || todo[0].ItemID != ep || todo[0].Duration != 1800 {
		t.Fatalf("to scan: %+v %v", todo, err)
	}
	s.SetDetectedMarker(ep, "intro", 10, 40)
	if err := s.SetDetectedCommercials(ep, [][2]float64{{600, 720}, {1200, 1260}}); err != nil {
		t.Fatal(err)
	}
	s.MarkCommercialsChecked(r.ID)
	if todo, _ := s.RecordingsNeedingCommercials(10); len(todo) != 0 {
		t.Fatalf("still to scan: %+v", todo)
	}
	count := func(kind string) int {
		ms, _ := s.Markers(ep)
		n := 0
		for _, m := range ms {
			if m.Kind == kind {
				n++
			}
		}
		return n
	}
	if count("commercial") != 2 || count("intro") != 1 {
		t.Fatal("markers not saved")
	}
	// Editing the intro by hand keeps the ad breaks.
	if err := s.SetManualMarkers(ep, []Marker{{Kind: "intro", Start: 5, End: 35}}); err != nil {
		t.Fatal(err)
	}
	if count("commercial") != 2 {
		t.Fatal("ad breaks lost to an intro edit")
	}
	// Breaks set by hand win over detection.
	s.SetManualMarkers(ep, []Marker{{Kind: "commercial", Start: 610, End: 700}})
	s.SetDetectedCommercials(ep, [][2]float64{{1, 2}})
	if ms, _ := s.Markers(ep); len(ms) != 1 || ms[0].Start != 610 || ms[0].Source != "manual" {
		t.Fatalf("manual breaks replaced: %+v", ms)
	}
}
