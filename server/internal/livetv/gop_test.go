package livetv

import (
	"os/exec"
	"testing"
)

func TestGOPCacheStartsAtKeyframe(t *testing.T) {
	ts, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine", "-t", "5", "-c:v", "libx264", "-g", "25", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-f", "mpegts", "pipe:1").Output()
	if err != nil {
		t.Skipf("ffmpeg: %v", err)
	}
	g := newGOPCache()
	if g.start() != nil {
		t.Fatal("a cache with no keyframe yet should have no start")
	}
	// Odd-sized chunks, so packets straddle them.
	for i := 0; i < len(ts); i += 10007 {
		g.feed(ts[i:min(i+10007, len(ts))])
	}
	if g.video < 0 {
		t.Fatal("didn't find the video PID")
	}
	start := g.start()
	if len(start) == 0 || len(start) > len(ts)/3 {
		t.Fatalf("start is %d of %d bytes; want the last second or so", len(start), len(ts))
	}
	pid := int(start[1]&0x1f)<<8 | int(start[2])
	if start[0] != 0x47 || pid != g.video || start[3]&0x20 == 0 || start[5]&0x40 == 0 {
		t.Fatalf("start isn't a video keyframe packet: % x", start[:8])
	}
	// What a joining player gets decodes from its first frame.
	cmd := exec.Command("ffprobe", "-v", "error", "-f", "mpegts", "-i", "pipe:0", "-select_streams", "v:0",
		"-show_entries", "frame=key_frame", "-read_intervals", "%+#1", "-of", "csv=p=0")
	in, _ := cmd.StdinPipe()
	go func() { in.Write(start); in.Close() }()
	out, err := cmd.Output()
	if err != nil || len(out) == 0 || out[0] != '1' {
		t.Fatalf("first frame isn't a keyframe: %q %v", out, err)
	}
}
