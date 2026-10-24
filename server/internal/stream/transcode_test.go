package stream

import (
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

func TestBuildArgsBurnIn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: "ffmpeg", dataDir: t.TempDir(), caps: ffmpegCaps{readrateBurst: true}}
	f := &store.MediaFile{
		Path: "movie.mkv", VideoCodec: "h264", BitDepth: 8, Height: 1080,
		Streams: []store.Stream{
			{Index: 0, Type: "video", Codec: "h264"},
			{Index: 1, Type: "audio", Codec: "ac3", Default: true},
			{Index: 2, Type: "subtitle", Codec: "hdmv_pgs_subtitle"},
			{Index: 3, Type: "subtitle", Codec: "subrip"},
		},
	}

	s := &Session{dir: "out"}
	args := strings.Join(m.buildArgs(f, s, Options{AudioStream: -1, BurnSubtitle: 2, MaxHeight: 720}), " ")
	if s.VideoCopy || s.BurnSubtitle != 2 {
		t.Fatalf("burning must force a re-encode: copy=%v burn=%d", s.VideoCopy, s.BurnSubtitle)
	}
	for _, want := range []string{
		"-filter_complex [0:v:0]null[base];[base][0:2]overlay=eof_action=pass[ov];[ov]scale=-2:720,format=yuv420p[vout]",
		"-map [vout] -map 0:1",
		"-readrate 2 -readrate_initial_burst 60",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}

	// Text subtitles are never burned; H.264 at source size is copied.
	s = &Session{dir: "out"}
	args = strings.Join(m.buildArgs(f, s, Options{AudioStream: -1, BurnSubtitle: 3}), " ")
	if !s.VideoCopy || s.BurnSubtitle != -1 || strings.Contains(args, "filter_complex") {
		t.Errorf("text subtitle should not be burned: copy=%v burn=%d\n%s", s.VideoCopy, s.BurnSubtitle, args)
	}
}
