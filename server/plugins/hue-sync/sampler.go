package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Sampler reads a file's picture ahead of the player with ffmpeg, at a few
// frames a second and 48×27 pixels, and keeps each frame's zone colours.
// It reads at most `ahead` seconds past the playhead, so after the first
// burst it decodes at the speed of playback.
type Sampler struct {
	from  float64 // file time of frames[0]
	fps   float64
	ahead float64

	mu       sync.Mutex
	frames   []map[string]Colour
	playhead float64
	err      error
	done     bool
	wake     chan struct{}
	cancel   context.CancelFunc
}

// ffmpegArgs reads url from `from` seconds as small raw RGB frames on stdout.
func ffmpegArgs(url, key string, from, fps float64) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if key != "" {
		args = append(args, "-headers", "Authorization: Bearer "+key+"\r\n")
	}
	if from > 0 {
		args = append(args, "-ss", strconv.FormatFloat(from, 'f', 2, 64))
	}
	// Two decode threads and skipped non-reference frames keep the CPU cost
	// down; colours don't need every frame.
	args = append(args, "-threads", "2", "-skip_frame", "noref", "-i", url,
		"-map", "0:v:0", "-an", "-sn", "-dn",
		"-vf", fmt.Sprintf("fps=%g,scale=%d:%d:flags=area,format=rgb24", fps, frameW, frameH),
		"-f", "rawvideo", "pipe:1")
	return args
}

func startSampler(ctx context.Context, ffmpeg, url, key string, from, fps, ahead float64) *Sampler {
	ctx, cancel := context.WithCancel(ctx)
	s := &Sampler{from: from, fps: fps, ahead: ahead, playhead: from, wake: make(chan struct{}, 1), cancel: cancel}
	cmd := exec.CommandContext(ctx, ffmpeg, ffmpegArgs(url, key, from, fps)...)
	stderr := &tail{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		s.finish(err)
		return s
	}
	go func() {
		r := bufio.NewReaderSize(out, frameW*frameH*3*4)
		buf := make([]byte, frameW*frameH*3)
		for {
			if !s.waitForRoom(ctx) {
				break
			}
			if _, err := io.ReadFull(r, buf); err != nil {
				break
			}
			z := zoneColours(buf)
			s.mu.Lock()
			s.frames = append(s.frames, z)
			// Forget what's well behind the playhead; a long film would otherwise pile up.
			if drop := int((s.playhead - 30 - s.from) * s.fps); drop > 0 && drop < len(s.frames) {
				s.frames = append([]map[string]Colour(nil), s.frames[drop:]...)
				s.from += float64(drop) / s.fps
			}
			s.mu.Unlock()
		}
		err := cmd.Wait()
		if ctx.Err() != nil {
			err = nil
		} else if err != nil && stderr.String() != "" {
			err = fmt.Errorf("ffmpeg: %s", stderr.String())
		}
		s.finish(err)
	}()
	return s
}

// waitForRoom blocks while the sampler is more than `ahead` seconds past the playhead.
func (s *Sampler) waitForRoom(ctx context.Context) bool {
	for {
		s.mu.Lock()
		end := s.from + float64(len(s.frames))/s.fps
		room := end < s.playhead+s.ahead
		s.mu.Unlock()
		if room {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-s.wake:
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Sampler) finish(err error) {
	s.mu.Lock()
	s.done, s.err = true, err
	s.mu.Unlock()
}

// At returns the colours of the frame showing at file time t.
func (s *Sampler) At(t float64) (map[string]Colour, bool) {
	s.mu.Lock()
	if t > s.playhead {
		s.playhead = t
	}
	defer s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	i := int((t - s.from) * s.fps)
	if i < 0 || i >= len(s.frames) {
		return nil, false
	}
	return s.frames[i], true
}

// Covers reports whether t is inside what's read or about to be read, so a
// small seek doesn't restart ffmpeg.
func (s *Sampler) Covers(t float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	end := s.from + float64(len(s.frames))/s.fps
	if s.done {
		return t >= s.from && t < end
	}
	return t >= s.from && t < end+s.ahead
}

func (s *Sampler) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Sampler) Stop() { s.cancel() }

// tail keeps the end of ffmpeg's error output.
type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 2000 {
		t.b = t.b[len(t.b)-2000:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(trimSpace(t.b))
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
