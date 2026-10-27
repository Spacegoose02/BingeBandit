package stream

import (
	"os/exec"
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

	// The viewer asked for a conversion: H.264 is encoded, not copied.
	s = &Session{dir: "out"}
	args = strings.Join(m.buildArgs(f, s, Options{AudioStream: -1, BurnSubtitle: -1, EncodeVideo: true}), " ")
	if s.VideoCopy || !strings.Contains(args, "-c:v libx264") {
		t.Errorf("encodeVideo should encode: copy=%v\n%s", s.VideoCopy, args)
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
		{"passthrough device gets DTS as Dolby Digital", dts, Options{AudioCodecs: []string{"aac", "ac3"}, MaxAudioChannels: 6, SurroundCodec: "ac3"},
			"-c:a ac3 -ac 6 -b:a 640k", "ac3", 6},
		{"or as Dolby Digital Plus", dts, Options{AudioCodecs: []string{"aac", "eac3"}, MaxAudioChannels: 6, SurroundCodec: "eac3"},
			"-c:a eac3 -ac 6 -b:a 768k", "eac3", 6},
		{"stereo source stays AAC", stereo, Options{MaxAudioChannels: 6, SurroundCodec: "ac3"}, "-c:a aac -ac 2 -b:a 192k", "aac", 2},
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

func TestBuildArgsAdaptiveBurnIn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSettings(map[string]string{store.SettingAdaptiveStreaming: "true", store.SettingHWEncoder: "nvenc"})
	m := &Manager{st: st, ffmpeg: "ffmpeg", dataDir: t.TempDir()}
	f := &store.MediaFile{
		Path: "movie.mkv", VideoCodec: "hevc", BitDepth: 10, Height: 2160,
		Streams: []store.Stream{
			{Index: 0, Type: "video", Codec: "hevc"},
			{Index: 1, Type: "audio", Codec: "eac3", Default: true},
			{Index: 2, Type: "subtitle", Codec: "hdmv_pgs_subtitle"},
		},
	}
	s := &Session{dir: "out"}
	args := strings.Join(m.buildArgs(f, s, Options{AudioStream: -1, BurnSubtitle: 2, Adaptive: true}), " ")
	if len(s.Variants) != 3 || s.Variants[0] != 2160 || s.Variants[2] != 720 {
		t.Fatalf("variants: %v", s.Variants)
	}
	for _, want := range []string{
		"[0:v:0]null[base];[base][0:2]overlay=eof_action=pass[ov];[ov]split=3[s0][s1][s2];[s0]format=yuv420p[v0];[s1]scale=-2:1080,format=yuv420p[v1];[s2]scale=-2:720,format=yuv420p[v2]",
		"-map [v0] -map 0:1 -map [v1] -map 0:1 -map [v2] -map 0:1",
		"-maxrate:v:1 10000k -bufsize:v:1 20000k -b:v:1 7500k",
		"-var_stream_map v:0,a:0 v:1,a:1 v:2,a:2 -master_pl_name master.m3u8",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}

	// A client that doesn't ask, or a file that can be copied, stays single.
	for _, o := range []Options{{AudioStream: -1, BurnSubtitle: -1}, {AudioStream: -1, BurnSubtitle: -1, Adaptive: true}} {
		s = &Session{dir: "out"}
		g := *f
		if o.Adaptive {
			g.VideoCodec, g.BitDepth, g.Height = "h264", 8, 1080
		}
		if args := strings.Join(m.buildArgs(&g, s, o), " "); len(s.Variants) != 0 || strings.Contains(args, "var_stream_map") {
			t.Errorf("should be single quality: %+v\n%s", o, args)
		}
	}
}

func TestBuildArgsHardwareDevice(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: "ffmpeg", dataDir: t.TempDir()}
	hdr := &store.MediaFile{Path: "movie.mkv", VideoCodec: "hevc", BitDepth: 10, HDR: true, Height: 2160,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "hevc"}, {Index: 1, Type: "audio", Codec: "aac", Channels: 2}}}
	sdr := &store.MediaFile{Path: "movie.mkv", VideoCodec: "h264", BitDepth: 8, Height: 1080,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}, {Index: 1, Type: "audio", Codec: "aac", Channels: 2}}}
	opts := Options{AudioStream: -1, BurnSubtitle: -1}
	for _, c := range []struct {
		name     string
		settings map[string]string
		file     *store.MediaFile
		want     []string
		not      []string
	}{
		{"defaults", map[string]string{store.SettingHWEncoder: "vaapi"}, hdr,
			[]string{"-vaapi_device /dev/dri/renderD128 -i", "tonemap=tonemap=hable:desat=0"}, []string{"-hwaccel"}},
		{"vaapi on a second GPU, decoding too", map[string]string{store.SettingHWEncoder: "vaapi", store.SettingHWDevice: "/dev/dri/renderD129",
			store.SettingHWDecode: "true", store.SettingTonemapAlgorithm: "mobius"}, hdr,
			[]string{"-vaapi_device /dev/dri/renderD129 -hwaccel vaapi -hwaccel_device /dev/dri/renderD129 -i", "tonemap=tonemap=mobius:desat=0"}, nil},
		{"nvenc GPU 1", map[string]string{store.SettingHWEncoder: "nvenc", store.SettingHWDevice: "1", store.SettingHWDecode: "true"}, hdr,
			[]string{"-hwaccel cuda -hwaccel_device 1 -i", "-c:v h264_nvenc -preset p4 -profile:v high -gpu 1"}, []string{"vaapi"}},
		{"qsv render node", map[string]string{store.SettingHWEncoder: "qsv", store.SettingHWDevice: "/dev/dri/renderD129", store.SettingHWDecode: "true"}, hdr,
			[]string{"-qsv_device /dev/dri/renderD129 -hwaccel auto -i"}, nil},
		{"copied video needs no GPU", map[string]string{store.SettingHWEncoder: "nvenc", store.SettingHWDecode: "true"}, sdr,
			[]string{"-c:v copy"}, []string{"-hwaccel", "-gpu"}},
		{"unknown algorithm falls back", map[string]string{store.SettingHWEncoder: "none", store.SettingTonemapAlgorithm: "bogus"}, hdr,
			[]string{"tonemap=tonemap=hable"}, nil},
	} {
		defaults := map[string]string{store.SettingHWDevice: "", store.SettingHWDecode: "false", store.SettingTonemapAlgorithm: "hable"}
		for k, v := range c.settings {
			defaults[k] = v
		}
		st.SetSettings(defaults)
		s := &Session{dir: "out"}
		args := strings.Join(m.buildArgs(c.file, s, opts), " ")
		for _, w := range c.want {
			if !strings.Contains(args, w) {
				t.Errorf("%s: missing %q:\n%s", c.name, w, args)
			}
		}
		for _, w := range c.not {
			if strings.Contains(args, w) {
				t.Errorf("%s: unexpected %q:\n%s", c.name, w, args)
			}
		}
	}
}

// Every algorithm an admin can pick is accepted by ffmpeg's tonemap filter.
func TestTonemapAlgorithmsWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	for alg := range TonemapAlgorithms {
		chain := "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=" + alg +
			":desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
		out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi",
			"-i", "testsrc=size=320x240:rate=5:duration=1,format=yuv420p10le,setparams=color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc",
			"-vf", chain, "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Errorf("%s: %v %s", alg, err, out)
		}
	}
}
