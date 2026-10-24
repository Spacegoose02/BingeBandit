// Package stream runs ffmpeg HLS transcodes and tracks who is watching what.
package stream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

const (
	segmentSeconds = 4
	idleTimeout    = 90 * time.Second
	waitTimeout    = 45 * time.Second
)

type Options struct {
	Start       float64 `json:"start"`       // seconds into the file
	MaxHeight   int     `json:"maxHeight"`   // 0 = source
	BitrateKbps int     `json:"bitrateKbps"` // 0 = quality-based
	AudioStream int     `json:"audioStream"` // ffprobe stream index, -1 = default
	// BurnSubtitle is an image subtitle stream index to draw into the
	// video (-1 = none). Text subtitles are rendered by the player instead.
	BurnSubtitle int    `json:"burnSubtitle"`
	Replace      string `json:"replace"` // session to stop first (the player is seeking)
	// AudioCodecs are the codecs the client can play inside HLS (e.g. "aac",
	// "ac3", "eac3"); a matching source track is copied instead of converted.
	// MaxAudioChannels is how many channels it can output (0 = stereo).
	// Older clients send neither and get AAC stereo.
	AudioCodecs      []string `json:"audioCodecs"`
	MaxAudioChannels int      `json:"maxAudioChannels"`
}

type Session struct {
	ID        string  `json:"id"`
	FileID    int64   `json:"fileId"`
	UserID    int64   `json:"userId"`
	Start     float64 `json:"start"`
	VideoCopy bool    `json:"videoCopy"`
	Encoder   string  `json:"encoder"`
	Height    int     `json:"height"`
	Tonemap   bool    `json:"tonemap"`
	// BurnSubtitle is the burned-in subtitle stream, or -1.
	BurnSubtitle int   `json:"burnSubtitle"`
	Throttled    bool  `json:"throttled"`
	Created      int64 `json:"created"`
	// AudioCodec and AudioChannels describe the audio sent to the client;
	// AudioCopy is set when it's the source track untouched.
	AudioCodec    string `json:"audioCodec"`
	AudioChannels int    `json:"audioChannels"`
	AudioCopy     bool   `json:"audioCopy"`

	dir        string
	cmd        *exec.Cmd
	done       chan struct{}
	lastAccess atomic.Int64
	stopped    atomic.Bool
	stderr     *tailBuffer
}

type Manager struct {
	st      *store.Store
	ffmpeg  string
	dataDir string

	caps     ffmpegCaps
	mu       sync.Mutex
	sessions map[string]*Session
	stop     chan struct{}
}

func NewManager(st *store.Store, ffmpeg, dataDir string) *Manager {
	m := &Manager{st: st, ffmpeg: ffmpeg, dataDir: dataDir, sessions: map[string]*Session{}, stop: make(chan struct{}),
		caps: probeCaps(ffmpeg)}
	m.cleanStale()
	go m.janitor()
	return m
}

func (m *Manager) baseDir() string {
	if d := strings.TrimSpace(m.st.Setting(store.SettingTranscodeDir)); d != "" {
		return d
	}
	return filepath.Join(m.dataDir, "transcode")
}

// cleanStale removes session folders left behind by a crash.
func (m *Manager) cleanStale() {
	entries, _ := os.ReadDir(m.baseDir())
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "bb-") {
			os.RemoveAll(filepath.Join(m.baseDir(), e.Name()))
		}
	}
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *Manager) Start(f *store.MediaFile, userID int64, o Options) (*Session, error) {
	if o.Replace != "" {
		m.Stop(o.Replace)
	}
	s := &Session{
		ID: newID(), FileID: f.ID, UserID: userID, Start: o.Start, Created: time.Now().Unix(),
		done: make(chan struct{}), stderr: &tailBuffer{max: 4096},
	}
	s.dir = filepath.Join(m.baseDir(), "bb-"+s.ID)
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("transcode dir: %w", err)
	}
	args := m.buildArgs(f, s, o)
	s.cmd = exec.Command(m.ffmpeg, args...)
	s.cmd.Stderr = s.stderr
	media.HideWindow(s.cmd)
	if err := s.cmd.Start(); err != nil {
		os.RemoveAll(s.dir)
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	s.lastAccess.Store(time.Now().Unix())
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	log.Printf("transcode %s: file %d start=%.0fs copy=%v encoder=%s height=%d burn=%d throttled=%v audio=%s/%dch copy=%v",
		s.ID, f.ID, o.Start, s.VideoCopy, s.Encoder, s.Height, s.BurnSubtitle, s.Throttled, s.AudioCodec, s.AudioChannels, s.AudioCopy)

	go func() {
		err := s.cmd.Wait()
		close(s.done)
		if s.stopped.Load() {
			return
		}
		if msg := strings.TrimSpace(s.stderr.String()); err != nil || msg != "" {
			log.Printf("transcode %s exited (%v): %s", s.ID, err, msg)
		}
	}()
	return s, nil
}

// imageSubtitleCodecs are bitmap subtitles browsers can't render; they are
// burned into the video instead.
var imageSubtitleCodecs = map[string]bool{"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true}

// IsImageSubtitle reports whether a subtitle codec has to be burned in.
func IsImageSubtitle(codec string) bool { return imageSubtitleCodecs[codec] }

func (m *Manager) buildArgs(f *store.MediaFile, s *Session, o Options) []string {
	encoder := m.st.Setting(store.SettingHWEncoder)
	tonemap := m.st.Setting(store.SettingTonemap) == "true" && f.HDR

	burn := -1
	for _, st := range f.Streams {
		if o.BurnSubtitle >= 0 && st.Index == o.BurnSubtitle && st.Type == "subtitle" && st.External == "" && IsImageSubtitle(st.Codec) {
			burn = st.Index
		}
	}

	targetH := f.Height
	if o.MaxHeight > 0 && (targetH == 0 || targetH > o.MaxHeight) {
		targetH = o.MaxHeight
	}
	s.Height = targetH
	s.VideoCopy = f.VideoCodec == "h264" && f.BitDepth <= 8 && !f.HDR && targetH == f.Height && o.BitrateKbps == 0 && burn < 0
	s.Tonemap = tonemap && !s.VideoCopy
	s.BurnSubtitle = burn

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}
	if encoder == "vaapi" && !s.VideoCopy {
		args = append(args, "-vaapi_device", "/dev/dri/renderD128")
	}
	if m.st.Setting(store.SettingTranscodeThrottle) == "true" && m.caps.readrateBurst {
		// Encode the first minute flat out so playback starts fast, then
		// stay about 2x ahead of real time instead of burning CPU on the
		// whole film.
		args = append(args, "-readrate", "2", "-readrate_initial_burst", "60")
		s.Throttled = true
	}
	if o.Start > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", o.Start))
	}
	args = append(args, "-i", f.Path)

	var videoArgs []string
	videoMap := "0:v:0"
	if s.VideoCopy {
		s.Encoder = "copy"
		videoArgs = []string{"-c:v", "copy"}
	} else {
		// pre runs on the source frames (before subtitles are overlaid),
		// post prepares the result for the encoder.
		var pre, post []string
		if s.Tonemap {
			pre = append(pre, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
				"tonemap=tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv")
		}
		if targetH > 0 && targetH < f.Height {
			post = append(post, fmt.Sprintf("scale=-2:%d", targetH))
		}
		var codec []string
		switch encoder {
		case "nvenc":
			post = append(post, "format=yuv420p")
			codec = []string{"-c:v", "h264_nvenc", "-preset", "p4", "-profile:v", "high"}
		case "qsv":
			post = append(post, "format=nv12")
			codec = []string{"-c:v", "h264_qsv", "-preset", "veryfast", "-profile:v", "high"}
		case "amf":
			post = append(post, "format=yuv420p")
			codec = []string{"-c:v", "h264_amf", "-quality", "speed"}
		case "vaapi":
			post = append(post, "format=nv12", "hwupload")
			codec = []string{"-c:v", "h264_vaapi"}
		case "videotoolbox":
			post = append(post, "format=yuv420p")
			codec = []string{"-c:v", "h264_videotoolbox", "-realtime", "1"}
		default:
			encoder = "libx264"
			post = append(post, "format=yuv420p")
			codec = []string{"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high", "-crf", "21"}
		}
		s.Encoder = encoder
		if burn >= 0 {
			graph := fmt.Sprintf("[0:v:0]%s[base];[base][0:%d]overlay=eof_action=pass[ov];[ov]%s[vout]",
				chain(pre), burn, chain(post))
			videoArgs = append(videoArgs, "-filter_complex", graph)
			videoMap = "[vout]"
		} else {
			videoArgs = append(videoArgs, "-vf", strings.Join(append(pre, post...), ","))
		}
		videoArgs = append(videoArgs, codec...)
		kbps := o.BitrateKbps
		if kbps == 0 {
			kbps = defaultBitrate(targetH)
		}
		videoArgs = append(videoArgs, "-maxrate", fmt.Sprintf("%dk", kbps), "-bufsize", fmt.Sprintf("%dk", kbps*2))
		if encoder != "libx264" {
			videoArgs = append(videoArgs, "-b:v", fmt.Sprintf("%dk", kbps*3/4))
		}
		videoArgs = append(videoArgs, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSeconds))
	}

	args = append(args, "-map", videoMap)
	audio := pickAudio(f.Streams, o.AudioStream)
	if audio != nil {
		args = append(args, "-map", fmt.Sprintf("0:%d", audio.Index))
	} else {
		args = append(args, "-map", "0:a:0?")
	}
	args = append(args, "-sn", "-map_metadata", "-1", "-map_chapters", "-1")
	args = append(args, videoArgs...)
	args = append(args, audioArgs(s, audio, o)...)
	args = append(args, "-f", "hls", "-hls_time", fmt.Sprint(segmentSeconds), "-hls_list_size", "0",
		"-hls_playlist_type", "event", "-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(s.dir, "seg%05d.ts"),
		"-start_number", "0", filepath.Join(s.dir, "index.m3u8"))
	return args
}

// pickAudio returns the audio stream to send: the requested index, else the
// file's default track. Nil when the file has no (known) audio streams.
func pickAudio(streams []store.Stream, index int) *store.Stream {
	if index >= 0 {
		for i, st := range streams {
			if st.Index == index && st.Type == "audio" {
				return &streams[i]
			}
		}
		return &store.Stream{Index: index, Type: "audio"} // unknown: convert to be safe
	}
	return media.DefaultAudio(streams)
}

// copyableAudio are the codecs that HLS in MPEG-TS can carry as they are.
var copyableAudio = map[string]bool{"aac": true, "ac3": true, "eac3": true, "mp3": true}

// audioArgs picks how to send the audio track: copy it when the client can
// play it as is, convert to AAC 5.1 for surround clients, else AAC stereo.
// It records the choice on s.
func audioArgs(s *Session, a *store.Stream, o Options) []string {
	maxCh := o.MaxAudioChannels
	if maxCh < 2 {
		maxCh = 2
	}
	if a != nil && a.Codec != "" && copyableAudio[a.Codec] && a.Channels > 0 && a.Channels <= maxCh {
		for _, c := range o.AudioCodecs {
			if c == a.Codec {
				s.AudioCodec, s.AudioChannels, s.AudioCopy = a.Codec, a.Channels, true
				return []string{"-c:a", "copy"}
			}
		}
	}
	if a != nil && a.Channels >= 6 && maxCh >= 6 {
		s.AudioCodec, s.AudioChannels = "aac", 6
		return []string{"-c:a", "aac", "-ac", "6", "-b:a", "384k"}
	}
	s.AudioCodec, s.AudioChannels = "aac", 2
	return []string{"-c:a", "aac", "-ac", "2", "-b:a", "192k"}
}

// chain joins filters, using the pass-through "null" filter when empty.
func chain(filters []string) string {
	if len(filters) == 0 {
		return "null"
	}
	return strings.Join(filters, ",")
}

type ffmpegCaps struct {
	readrateBurst bool // -readrate_initial_burst (ffmpeg 6.1+)
}

// probeCaps asks ffmpeg which optional features it has, so older builds
// (e.g. distro packages) keep working without them.
func probeCaps(ffmpeg string) ffmpegCaps {
	cmd := exec.Command(ffmpeg, "-hide_banner", "-h", "full")
	media.HideWindow(cmd)
	out, _ := cmd.Output()
	return ffmpegCaps{readrateBurst: bytes.Contains(out, []byte("readrate_initial_burst"))}
}

func defaultBitrate(height int) int {
	switch {
	case height >= 2160:
		return 40000
	case height >= 1440:
		return 20000
	case height >= 1080:
		return 10000
	case height >= 720:
		return 5000
	default:
		return 2500
	}
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) Stop(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		s.kill()
	}
}

func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

func (m *Manager) Shutdown() {
	close(m.stop)
	for _, s := range m.List() {
		m.Stop(s.ID)
	}
}

func (m *Manager) janitor() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		cutoff := time.Now().Add(-idleTimeout).Unix()
		for _, s := range m.List() {
			if s.lastAccess.Load() < cutoff {
				log.Printf("transcode %s: idle, stopping", s.ID)
				m.Stop(s.ID)
			}
		}
	}
}

func (s *Session) kill() {
	s.stopped.Store(true)
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	go func() {
		select {
		case <-s.done:
		case <-time.After(10 * time.Second):
		}
		// Windows may hold the last segment open briefly after the kill.
		for i := 0; i < 10; i++ {
			if err := os.RemoveAll(s.dir); err == nil {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

var reSegment = regexp.MustCompile(`^(index\.m3u8|seg\d{5}\.ts)$`)

var ErrNotReady = errors.New("transcode output not ready")

// WaitFile blocks until the playlist has a first segment, or until the
// requested segment is complete (listed in the playlist or ffmpeg finished).
func (s *Session) WaitFile(ctx context.Context, name string) (string, error) {
	if !reSegment.MatchString(name) {
		return "", os.ErrNotExist
	}
	s.lastAccess.Store(time.Now().Unix())
	playlist := filepath.Join(s.dir, "index.m3u8")
	path := filepath.Join(s.dir, name)
	deadline := time.After(waitTimeout)
	for {
		pl, _ := readShared(playlist)
		finished := false
		select {
		case <-s.done:
			finished = true
		default:
		}
		if name == "index.m3u8" {
			if bytes.Contains(pl, []byte("#EXTINF")) {
				return path, nil
			}
		} else if bytes.Contains(pl, []byte(name)) {
			return path, nil
		} else if finished {
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
		if finished {
			return "", fmt.Errorf("%w: ffmpeg exited: %s", ErrNotReady, strings.TrimSpace(s.stderr.String()))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", ErrNotReady
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func readShared(path string) ([]byte, error) {
	f, err := OpenShared(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// tailBuffer keeps the last max bytes written to it (ffmpeg stderr).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
