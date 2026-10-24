package livetv

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/media"
)

// renderDevice is the GPU that VAAPI and Quick Sync open on Linux (the same
// one the on-demand transcoder uses).
const renderDevice = "/dev/dri/renderD128"

// hwRetryAfter is how long a hardware encoder that failed is skipped for live
// TV before it is tried again.
const hwRetryAfter = 30 * time.Minute

// encodeArgs returns the ffmpeg arguments that go before the input (device
// setup) and after it (filters and encoder) to turn video into H.264 with the
// chosen encoder. vf is the software filter chain; it must end in a pixel
// format the encoder takes, which this adjusts per encoder.
func encodeArgs(hw, vf, bitrate, maxrate string) (in, out []string) {
	nv12 := strings.Replace(vf, "format=yuv420p", "format=nv12", 1)
	rate := []string{"-b:v", bitrate, "-maxrate", maxrate, "-bufsize", maxrate}
	switch hw {
	case "nvenc":
		out = append([]string{"-vf", vf, "-c:v", "h264_nvenc", "-preset", "p4"}, rate...)
	case "qsv":
		if runtime.GOOS == "linux" {
			// Without an explicit device, Quick Sync inside Docker often can't
			// find the GPU (or picks the wrong one) and ffmpeg exits at once.
			in = []string{"-init_hw_device", "vaapi=va:" + renderDevice, "-init_hw_device", "qsv=qs@va", "-filter_hw_device", "qs"}
			out = []string{"-vf", nv12 + ",hwupload=extra_hw_frames=64,format=qsv"}
		} else {
			out = []string{"-vf", nv12}
		}
		out = append(append(out, "-c:v", "h264_qsv", "-preset", "veryfast"), rate...)
	case "vaapi":
		in = []string{"-vaapi_device", renderDevice}
		out = append([]string{"-vf", nv12 + ",hwupload", "-c:v", "h264_vaapi"}, rate...)
	case "amf":
		out = append([]string{"-vf", vf, "-c:v", "h264_amf", "-quality", "speed"}, rate...)
	case "videotoolbox":
		out = append([]string{"-vf", vf, "-c:v", "h264_videotoolbox", "-realtime", "1"}, rate...)
	default:
		out = []string{"-vf", vf, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-crf", "22", "-maxrate", maxrate, "-bufsize", maxrate}
	}
	return in, out
}

// hwHealth remembers hardware encoders that just failed on live TV so the
// next start uses the CPU instead of failing the same way again.
type hwHealth struct {
	mu     sync.Mutex
	failed map[string]time.Time
	reason map[string]string
}

// pick returns the encoder to use: the configured one, or "none" while it is
// marked as failing.
func (h *hwHealth) pick(hw string) string {
	if hw == "" || hw == "none" {
		return "none"
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if at, ok := h.failed[hw]; ok && time.Since(at) < hwRetryAfter {
		return "none"
	}
	return hw
}

func (h *hwHealth) markFailed(hw, why string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failed == nil {
		h.failed, h.reason = map[string]time.Time{}, map[string]string{}
	}
	if _, already := h.failed[hw]; !already || time.Since(h.failed[hw]) >= hwRetryAfter {
		log.Printf("live tv: hardware encoder %s failed, using the CPU for live TV for the next %s: %s", hw, hwRetryAfter, why)
	}
	h.failed[hw], h.reason[hw] = time.Now(), why
}

// lastFailure reports why an encoder was last set aside, if it currently is.
func (h *hwHealth) lastFailure(hw string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if at, ok := h.failed[hw]; ok && time.Since(at) < hwRetryAfter {
		return h.reason[hw]
	}
	return ""
}

func (h *hwHealth) clear(hw string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.failed, hw)
	delete(h.reason, hw)
}

// TestEncoder encodes two seconds of a generated interlaced test picture with
// the given encoder, exactly as live TV would, and returns ffmpeg's error if
// it fails. A pass also clears an earlier failure so live TV tries it again.
func (m *Manager) TestEncoder(ctx context.Context, hw string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	in, out := encodeArgs(hw, liveFilter, "4M", "6M")
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	args = append(args, in...)
	args = append(args, "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=30000/1001,setfield=tff", "-t", "2")
	args = append(args, out...)
	args = append(args, "-an", "-f", "null", "-")
	cmd := exec.CommandContext(ctx, m.ffmpeg, args...)
	media.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", lastLines(msg, 6))
	}
	m.hw.clear(hw)
	return nil
}

// lastLines keeps the end of ffmpeg's output, where the real error is.
func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
