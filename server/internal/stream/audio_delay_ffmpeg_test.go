package stream

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"bingebandit/internal/store"
)

// Runs real ffmpeg on a clip that beeps on every whole second: started
// mid-file, the sound lands where the picture says it should, and an audio
// delay moves it by that much, whether the audio is copied or converted.
func TestAudioDelayWithFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	// Keyframes every 5 s, so starting at 7.3 s copies video from 5 s.
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=25:duration=16",
		"-f", "lavfi", "-i", "aevalsrc='if(lt(mod(t,1),0.05),sin(2*PI*1000*t),0)':s=48000:d=16",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-g", "125", "-keyint_min", "125", "-sc_threshold", "0",
		"-c:a", "ac3", "-ac", "2", clip).CombinedOutput(); err != nil {
		t.Skipf("could not make a test clip: %v %s", err, out)
	}
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := NewManager(st, ffmpeg, dir)
	defer m.Shutdown()
	f := &store.MediaFile{ID: 1, Path: clip, VideoCodec: "h264", BitDepth: 8, Width: 160, Height: 120, Duration: 16,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}, {Index: 1, Type: "audio", Codec: "ac3", Channels: 2, Default: true}}}

	packets := func(file, sel string) []float64 {
		out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", sel, "-show_entries", "packet=pts_time", "-of", "csv=p=0", file).Output()
		if err != nil {
			t.Fatal(err)
		}
		var ts []float64
		for _, l := range strings.Fields(string(out)) {
			if v, err := strconv.ParseFloat(strings.Trim(l, ","), 64); err == nil {
				ts = append(ts, v)
			}
		}
		sort.Float64s(ts)
		return ts
	}
	srcVideo := packets(clip, "v")

	// offset returns how far the first beep is from where the picture puts
	// it, in seconds (positive: the sound is late).
	offset := func(o Options) float64 {
		t.Helper()
		s, err := m.Start(f, 1, o)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-s.done:
		case <-time.After(60 * time.Second):
			t.Fatal("ffmpeg did not finish")
		}
		segs, _ := filepath.Glob(filepath.Join(s.dir, "seg*.ts"))
		sort.Strings(segs)
		all := filepath.Join(dir, "all.ts")
		var joined []byte
		for _, p := range segs {
			b, _ := os.ReadFile(p)
			joined = append(joined, b...)
		}
		if err := os.WriteFile(all, joined, 0o644); err != nil {
			t.Fatal(err)
		}
		// The copied video is the source's frames from a keyframe on, so
		// the output's last frames line up with the source's.
		outVideo := packets(all, "v")
		if len(outVideo) == 0 || len(outVideo) > len(srcVideo) {
			t.Fatalf("video frames: %d of %d (%s)", len(outVideo), len(srcVideo), s.stderr)
		}
		shift := outVideo[0] - srcVideo[len(srcVideo)-len(outVideo)] // output time - file time
		pcm, err := exec.Command(ffmpeg, "-v", "error", "-i", all, "-map", "0:a", "-f", "s16le", "-ac", "1", "-ar", "48000", "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		a0 := packets(all, "a")[0]
		quiet := 0
		for i := 0; i+1 < len(pcm); i += 2 {
			v := int16(binary.LittleEndian.Uint16(pcm[i:]))
			if v > 3000 || v < -3000 {
				if quiet > 24000 { // a beep after half a second of silence
					file := a0 + float64(i/2)/48000 - shift
					return file - math.Round(file)
				}
				quiet = 0
			} else {
				quiet++
			}
		}
		t.Fatal("no beep found")
		return 0
	}
	copyAudio := Options{AudioCodecs: []string{"aac", "ac3"}, MaxAudioChannels: 2}
	for _, c := range []struct {
		name string
		o    Options
		want float64
	}{
		{"converted", Options{}, 0},
		{"converted, 300ms later", Options{AudioDelayMs: 300}, 0.3},
		{"converted, 300ms earlier", Options{AudioDelayMs: -300}, -0.3},
		{"copied", copyAudio, 0},
		{"copied, 200ms later", Options{AudioCodecs: copyAudio.AudioCodecs, MaxAudioChannels: 2, AudioDelayMs: 200}, 0.2},
		{"copied, 400ms earlier", Options{AudioCodecs: copyAudio.AudioCodecs, MaxAudioChannels: 2, AudioDelayMs: -400}, -0.4},
	} {
		c.o.Start, c.o.AudioStream, c.o.BurnSubtitle = 7.3, -1, -1
		if got := offset(c.o); math.Abs(got-c.want) > 0.03 {
			t.Errorf("%s: sound %+.3fs from the picture, want %+.3fs", c.name, got, c.want)
		}
	}
}

func TestBuildArgsAudioDelay(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := &Manager{st: st, ffmpeg: "ffmpeg", dataDir: t.TempDir()}
	f := &store.MediaFile{ID: 1, Path: "/m/a.mkv", VideoCodec: "h264", BitDepth: 8, Height: 1080,
		Streams: []store.Stream{{Index: 0, Type: "video", Codec: "h264"}, {Index: 2, Type: "audio", Codec: "dts", Channels: 6, Default: true}}}
	s := &Session{dir: t.TempDir()}
	args := strings.Join(m.buildArgs(f, s, Options{Start: 60, AudioStream: -1, BurnSubtitle: -1, AudioDelayMs: -9000}), " ")
	for _, want := range []string{"-ss 60.000 -i /m/a.mkv -ss 60.000 -itsoffset -5.000 -i /m/a.mkv", "-map 0:v:0 -map 1:2", "-af aresample=async=1"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}
	if s.AudioDelayMs != -MaxAudioDelayMs {
		t.Errorf("delay %d, want clamped to %d", s.AudioDelayMs, -MaxAudioDelayMs)
	}
	s = &Session{dir: t.TempDir()}
	args = strings.Join(m.buildArgs(f, s, Options{AudioStream: -1, BurnSubtitle: -1}), " ")
	if strings.Count(args, "-i ") != 1 || !strings.Contains(args, "-map 0:2") || s.AudioDelayMs != 0 {
		t.Errorf("no delay should open the file once:\n%s", args)
	}
}
