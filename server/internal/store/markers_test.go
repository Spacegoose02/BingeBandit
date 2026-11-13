package store

import "testing"

// A replaced file (an upgrade, another cut) loses the intro and credits
// found in the old one's audio and is looked at again; times set by hand
// and a recording's ad breaks stay.
func TestReplacedFileForgetsDetectedMarkers(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id, err := s.UpsertItem(&Item{LibraryID: lib.ID, Kind: "movie", Title: "Film", MatchKey: "movie:film"})
	if err != nil {
		t.Fatal(err)
	}
	f := &MediaFile{ItemID: id, Path: "/m/film.mkv", Size: 100, MTime: 1, Duration: 5400}
	if err := s.UpsertFile(f, 1); err != nil {
		t.Fatal(err)
	}
	s.SetDetectedMarker(id, "credits", 5000, 5400)
	s.SetDetectedMarker(id, "intro", 10, 60)
	s.MarkMarkersScanned([]int64{id})
	kinds := func() map[string]string {
		ms, _ := s.Markers(id)
		out := map[string]string{}
		for _, m := range ms {
			out[m.Kind] = m.Source
		}
		return out
	}
	pending := func() bool {
		ids, _ := s.MoviesNeedingMarkers()
		return len(ids) == 1 && ids[0] == id
	}

	// The same file scanned again keeps them.
	if err := s.UpsertFile(f, 2); err != nil {
		t.Fatal(err)
	}
	if k := kinds(); len(k) != 2 || pending() {
		t.Fatalf("unchanged file: markers %v, pending %v", k, pending())
	}

	// A new copy: detected ones go, a manual intro stays.
	if err := s.SetManualMarkers(id, []Marker{{Kind: "intro", Start: 5, End: 50}}); err != nil {
		t.Fatal(err)
	}
	s.SetDetectedMarker(id, "credits", 5000, 5400)
	s.MarkMarkersScanned([]int64{id})
	f.Size = 200
	if err := s.UpsertFile(f, 3); err != nil {
		t.Fatal(err)
	}
	if k := kinds(); len(k) != 1 || k["intro"] != "manual" {
		t.Errorf("replaced file: markers %v, want only the manual intro", k)
	}
	if !pending() {
		t.Error("replaced file isn't looked at again")
	}
}
