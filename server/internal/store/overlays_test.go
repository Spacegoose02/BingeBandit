package store

import "testing"

func TestFilesNeedingRange(t *testing.T) {
	s := openTest(t)
	lib := &Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	s.CreateLibrary(lib)
	id := addMovie(t, s, lib.ID, "A")
	s.UpsertFile(&MediaFile{ItemID: id, Path: "/old.mkv", VideoCodec: "hevc", ProbedAt: 5, Streams: []Stream{{Type: "video"}}}, 1)
	s.UpsertFile(&MediaFile{ItemID: id, Path: "/new.mkv", VideoCodec: "hevc", ProbedAt: 5, Streams: []Stream{{Type: "video", Range: "sdr"}}}, 1)
	s.UpsertFile(&MediaFile{ItemID: id, Path: "/unread.mkv", Streams: []Stream{}}, 1)
	files, err := s.FilesNeedingRange(10)
	if err != nil || len(files) != 1 || files[0].Path != "/old.mkv" {
		t.Fatalf("%v %v", files, err)
	}
	files[0].Streams[0].Range = "hdr10"
	s.UpdateProbe(files[0])
	if files, _ := s.FilesNeedingRange(10); len(files) != 0 {
		t.Errorf("still needs range: %v", files)
	}
}
