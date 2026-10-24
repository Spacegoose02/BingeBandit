package stream

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestAudioArgs(t *testing.T) {
	eac3 := &store.Stream{Index: 1, Type: "audio", Codec: "eac3", Channels: 6}
	dts := &store.Stream{Index: 2, Type: "audio", Codec: "dts", Channels: 8}
	stereo := &store.Stream{Index: 3, Type: "audio", Codec: "aac", Channels: 2}
	surround := Options{AudioCodecs: []string{"aac", "ac3", "eac3"}, MaxAudioChannels: 6}
	for _, c := range []struct {
		name  string
		a     *store.Stream
		o     Options
		want  string
		codec string
		ch    int
	}{
		{"old client gets stereo", eac3, Options{}, "-c:a aac -ac 2 -b:a 192k", "aac", 2},
		{"surround client gets the source", eac3, surround, "-c:a copy", "eac3", 6},
		{"codec it can't play becomes AAC 5.1", dts, surround, "-c:a aac -ac 6 -b:a 384k", "aac", 6},
		{"stereo output can't take 5.1", eac3, Options{AudioCodecs: []string{"eac3"}, MaxAudioChannels: 2}, "-c:a aac -ac 2 -b:a 192k", "aac", 2},
		{"stereo AAC is copied", stereo, Options{AudioCodecs: []string{"aac"}}, "-c:a copy", "aac", 2},
		{"unknown channels are converted", &store.Stream{Index: 4, Type: "audio", Codec: "ac3"}, surround, "-c:a aac -ac 2 -b:a 192k", "aac", 2},
		{"no audio", nil, surround, "-c:a aac -ac 2 -b:a 192k", "aac", 2},
	} {
		s := &Session{}
		got := strings.Join(audioArgs(s, c.a, c.o), " ")
		if got != c.want || s.AudioCodec != c.codec || s.AudioChannels != c.ch || s.AudioCopy != (c.want == "-c:a copy") {
			t.Errorf("%s: got %q (%s %d copy=%v), want %q", c.name, got, s.AudioCodec, s.AudioChannels, s.AudioCopy, c.want)
		}
	}
}

func TestAccessCheckDue(t *testing.T) {
	s := &Session{}
	s.accessChecked.Store(time.Now().UnixNano())
	if s.AccessCheckDue(time.Minute) {
		t.Fatal("due right after a check")
	}
	s.accessChecked.Store(time.Now().Add(-2 * time.Minute).UnixNano())
	if !s.AccessCheckDue(time.Minute) {
		t.Fatal("not due after the interval")
	}
	if s.AccessCheckDue(time.Minute) {
		t.Fatal("due twice in a row")
	}
}
