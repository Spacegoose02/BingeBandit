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
	// Adaptive asks for several qualities at once (a master playlist the
	// player switches between). Used only when the admin allows it and the
	// video has to be converted anyway.
	Adaptive bool `json:"adaptive"`
	// AudioCodecs are the codecs the client can play inside HLS (e.g. "aac",
	// "ac3", "eac3"); a matching source track is copied instead of converted.
	// MaxAudioChannels is how many channels it can output (0 = stereo).
	// Older clients send neither and get AAC stereo.
	AudioCodecs      []string `json:"audioCodecs"`
	MaxAudioChannels int      `json:"maxAudioChannels"`
	// SurroundCodec is what a surround track the client can't take as it
	// is (DTS, TrueHD) becomes: "ac3" or "eac3" for a device that passes
	// Dolby through to a receiver or soundbar (which often can't take
	// multichannel PCM over HDMI ARC). Empty: AAC 5.1.
	SurroundCodec string `json:"surroundCodec"`
	// EncodeVideo converts H.264 that would otherwise be copied: the viewer
	// picked "Convert", or direct play stuttered (a high bitrate, level or
	// profile the device can't keep up with).
	EncodeVideo bool `json:"encodeVideo"`
	// CapKbps is the most video this viewer may be sent (the admin's cap
	// for streams outside the home network); 0 = no cap. Set by the
	// server, never by the client.
	CapKbps int `json:"-"`
	// Remote marks a viewer outside the home network, for the per-user
	// stream limit. Set by the server.
	Remote bool `json:"-"`
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
	HWDecode     bool  `json:"hwDecode"` // decoding on the GPU was asked for
	Created      int64 `json:"created"`
	// Variants are the heights of an adaptive session, best first; empty
	// for a single-quality one.
	Variants []int `json:"variants,omitempty"`
	// AudioCodec and AudioChannels describe the audio sent to the client;
	// AudioCopy is set when it's the source track untouched.
	AudioCodec    string `json:"audioCodec"`
	AudioChannels int    `json:"audioChannels"`
	AudioCopy     bool   `json:"audioCopy"`
	// Remote is set when the viewer is outside the home network.
	Remote bool `json:"-"`

	dir        string
	cmd        *exec.Cmd
	done       chan struct{}
	lastAccess atomic.Int64
	// accessChecked is when the user's library access was last confirmed
	// (unix nanoseconds).
	accessChecked atomic.Int64
	stopped       atomic.Bool
	stderr        *tailBuffer
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
		ID: newID(), FileID: f.ID, UserID: userID, Start: o.Start, Created: time.Now().Unix(), Remote: o.Remote,
		done: make(chan struct{}), stderr: &tailBuffer{max: 4096},
	}
	// The caller checked access just before starting.
	s.accessChecked.Store(time.Now().UnixNano())
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
	// Over the cap (or of unknown size): convert, at a height that looks
	// good at that bitrate.
	overCap := o.CapKbps > 0 && (FileKbps(f) == 0 || FileKbps(f) > o.CapKbps)
	if h := HeightForKbps(o.CapKbps); overCap && (targetH == 0 || targetH > h) {
		targetH = h
	}
	s.Height = targetH
	s.VideoCopy = !o.EncodeVideo && !overCap && f.VideoCodec == "h264" && f.BitDepth <= 8 && !f.HDR && targetH == f.Height && o.BitrateKbps == 0 && burn < 0
	s.Tonemap = tonemap && !s.VideoCopy
	s.BurnSubtitle = burn

	device := m.st.Setting(store.SettingHWDevice)
	gpuIndex := device != "" && !strings.HasPrefix(device, "/")
	renderNode := "/dev/dri/renderD128"
	if device != "" && !gpuIndex {
		renderNode = device
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y"}
	if !s.VideoCopy {
		switch encoder {
		case "vaapi":
			args = append(args, "-vaapi_device", renderNode)
		case "qsv":
			if device != "" && !gpuIndex {
				args = append(args, "-qsv_device", renderNode)
			}
		}
		// Decoding on the GPU too. Frames come back to system memory, so the
		// filters below work unchanged, and ffmpeg decodes in software when
		// the GPU can't handle the codec.
		if m.st.Setting(store.SettingHWDecode) == "true" {
			switch encoder {
			case "nvenc":
				args = append(args, "-hwaccel", "cuda")
				if gpuIndex {
					args = append(args, "-hwaccel_device", device)
				}
			case "vaapi":
				args = append(args, "-hwaccel", "vaapi", "-hwaccel_device", renderNode)
			case "videotoolbox":
				args = append(args, "-hwaccel", "videotoolbox")
			case "qsv", "amf":
				args = append(args, "-hwaccel", "auto")
			}
			s.HWDecode = encoder != "none" && encoder != ""
		}
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

	audio := pickAudio(f.Streams, o.AudioStream)
	audioMap := "0:a:0?"
	if audio != nil {
		audioMap = fmt.Sprintf("0:%d", audio.Index)
	}
	hasAudio := audio != nil || len(f.Streams) == 0 // probed files with no audio track have none to map

	// The quality ladder: the target, then up to two lower rungs. Only when
	// converting anyway (a copied stream can't be lined up with encoded
	// ones) and the file has been probed.
	heights := []int{targetH}
	if o.Adaptive && !s.VideoCopy && targetH > 0 && len(f.Streams) > 0 && m.st.Setting(store.SettingAdaptiveStreaming) == "true" {
		for _, h := range adaptiveLadder {
			if h < targetH && len(heights) < 3 {
				heights = append(heights, h)
			}
		}
	}
	if len(heights) > 1 {
		s.Variants = heights
	}

	var videoArgs []string
	videoMaps := []string{"0:v:0"}
	if s.VideoCopy {
		s.Encoder = "copy"
		videoArgs = []string{"-c:v", "copy"}
	} else {
		// pre runs on the source frames (before subtitles are overlaid),
		// post prepares each output for the encoder.
		var pre, post []string
		if s.Tonemap {
			pre = append(pre, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
				"tonemap=tonemap="+tonemapAlgorithm(m.st.Setting(store.SettingTonemapAlgorithm))+":desat=0", "zscale=t=bt709:m=bt709:r=tv")
		}
		var codec []string
		switch encoder {
		case "nvenc":
			post = append(post, "format=yuv420p")
			codec = []string{"-c:v", "h264_nvenc", "-preset", "p4", "-profile:v", "high"}
			if gpuIndex {
				codec = append(codec, "-gpu", device)
			}
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
		// outputFilters scales one output to h, then readies it for the encoder.
		outputFilters := func(h int) []string {
			if h > 0 && h < f.Height {
				return append([]string{fmt.Sprintf("scale=-2:%d", h)}, post...)
			}
			return post
		}
		switch {
		case len(heights) > 1:
			graph := fmt.Sprintf("[0:v:0]%s[base]", chain(pre))
			src := "base"
			if burn >= 0 {
				graph += fmt.Sprintf(";[base][0:%d]overlay=eof_action=pass[ov]", burn)
				src = "ov"
			}
			graph += fmt.Sprintf(";[%s]split=%d", src, len(heights))
			for i := range heights {
				graph += fmt.Sprintf("[s%d]", i)
			}
			videoMaps = nil
			for i, h := range heights {
				graph += fmt.Sprintf(";[s%d]%s[v%d]", i, chain(outputFilters(h)), i)
				videoMaps = append(videoMaps, fmt.Sprintf("[v%d]", i))
			}
			videoArgs = append(videoArgs, "-filter_complex", graph)
		case burn >= 0:
			graph := fmt.Sprintf("[0:v:0]%s[base];[base][0:%d]overlay=eof_action=pass[ov];[ov]%s[vout]",
				chain(pre), burn, chain(outputFilters(targetH)))
			videoArgs = append(videoArgs, "-filter_complex", graph)
			videoMaps = []string{"[vout]"}
		default:
			videoArgs = append(videoArgs, "-vf", strings.Join(append(pre, outputFilters(targetH)...), ","))
		}
		videoArgs = append(videoArgs, codec...)
		for i, h := range heights {
			kbps := defaultBitrate(h)
			if i == 0 && o.BitrateKbps > 0 {
				kbps = o.BitrateKbps
			}
			if o.CapKbps > 0 && kbps > o.CapKbps {
				kbps = o.CapKbps
			}
			spec := ""
			if len(heights) > 1 {
				spec = fmt.Sprintf(":v:%d", i)
			}
			videoArgs = append(videoArgs, "-maxrate"+spec, fmt.Sprintf("%dk", kbps), "-bufsize"+spec, fmt.Sprintf("%dk", kbps*2))
			if encoder != "libx264" {
				videoArgs = append(videoArgs, "-b"+spec, fmt.Sprintf("%dk", kbps*3/4))
			}
		}
		videoArgs = append(videoArgs, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSeconds))
	}

	// Each variant carries its own copy of the audio, so every playlist
	// plays on its own.
	for _, vm := range videoMaps {
		args = append(args, "-map", vm)
		if hasAudio {
			args = append(args, "-map", audioMap)
		}
	}
	args = append(args, "-sn", "-map_metadata", "-1", "-map_chapters", "-1")
	args = append(args, videoArgs...)
	if hasAudio {
		args = append(args, audioArgs(s, audio, o)...)
	}
	args = append(args, "-f", "hls", "-hls_time", fmt.Sprint(segmentSeconds), "-hls_list_size", "0",
		"-hls_playlist_type", "event", "-hls_flags", "independent_segments", "-start_number", "0")
	if len(heights) > 1 {
		var streamMap []string
		for i := range heights {
			v := fmt.Sprintf("v:%d", i)
			if hasAudio {
				v += fmt.Sprintf(",a:%d", i)
			}
			streamMap = append(streamMap, v)
		}
		args = append(args, "-var_stream_map", strings.Join(streamMap, " "), "-master_pl_name", "master.m3u8",
			"-hls_segment_filename", filepath.Join(s.dir, "v%v_seg%05d.ts"), filepath.Join(s.dir, "v%v_index.m3u8"))
	} else {
		args = append(args, "-hls_segment_filename", filepath.Join(s.dir, "seg%05d.ts"), filepath.Join(s.dir, "index.m3u8"))
	}
	return args
}

// adaptiveLadder are the lower qualities an adaptive session adds below its
// target height.
var adaptiveLadder = []int{1080, 720, 480}

// Playlist is the playlist a player opens: the master playlist of an
// adaptive session, else the single one.
func (s *Session) Playlist() string {
	if len(s.Variants) > 1 {
		return "master.m3u8"
	}
	return "index.m3u8"
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
// play it as is, convert to 5.1 (Dolby Digital (Plus) when the client asked
// for it, else AAC) for surround clients, else AAC stereo.
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
		switch o.SurroundCodec {
		case "ac3":
			s.AudioCodec, s.AudioChannels = "ac3", 6
			return []string{"-c:a", "ac3", "-ac", "6", "-b:a", "640k"}
		case "eac3":
			s.AudioCodec, s.AudioChannels = "eac3", 6
			return []string{"-c:a", "eac3", "-ac", "6", "-b:a", "768k"}
		}
		s.AudioCodec, s.AudioChannels = "aac", 6
		return []string{"-c:a", "aac", "-ac", "6", "-b:a", "384k"}
	}
	s.AudioCodec, s.AudioChannels = "aac", 2
	return []string{"-c:a", "aac", "-ac", "2", "-b:a", "192k"}
}

// TonemapAlgorithms are the HDR → SDR curves an admin can pick.
var TonemapAlgorithms = map[string]bool{"hable": true, "mobius": true, "reinhard": true, "clip": true, "linear": true}

func tonemapAlgorithm(v string) string {
	if TonemapAlgorithms[v] {
		return v
	}
	return "hable"
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

// FileKbps is a file's overall bitrate in kbit/s, from the probe or else
// its size and length; 0 when neither is known.
func FileKbps(f *store.MediaFile) int {
	if f.Bitrate > 0 {
		return int(f.Bitrate / 1000)
	}
	if f.Size > 0 && f.Duration > 0 {
		return int(float64(f.Size) * 8 / f.Duration / 1000)
	}
	return 0
}

// HeightForKbps is the tallest picture that still looks good at kbps: one
// whose usual bitrate is at most twice that. 0 for no cap.
func HeightForKbps(kbps int) int {
	if kbps <= 0 {
		return 0
	}
	for _, h := range []int{2160, 1440, 1080, 720} {
		if defaultBitrate(h) <= kbps*2 {
			return h
		}
	}
	return 480
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

// AccessCheckDue reports whether the user's library access should be checked
// again (at most once per every), and marks it checked when it is.
func (s *Session) AccessCheckDue(every time.Duration) bool {
	now := time.Now().UnixNano()
	last := s.accessChecked.Load()
	return now-last >= int64(every) && s.accessChecked.CompareAndSwap(last, now)
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

var reSegment = regexp.MustCompile(`^(index\.m3u8|seg\d{5}\.ts|master\.m3u8|v\d_index\.m3u8|v\d_seg\d{5}\.ts)$`)

var ErrNotReady = errors.New("transcode output not ready")

// WaitFile blocks until the playlist has a first segment, or until the
// requested segment is complete (listed in the playlist or ffmpeg finished).
func (s *Session) WaitFile(ctx context.Context, name string) (string, error) {
	if !reSegment.MatchString(name) {
		return "", os.ErrNotExist
	}
	s.lastAccess.Store(time.Now().Unix())
	// The playlist that lists name: v1_seg00003.ts is in v1_index.m3u8. The
	// master playlist is ready once the first variant has a segment.
	playlist := filepath.Join(s.dir, "index.m3u8")
	if v, _, ok := strings.Cut(name, "_"); ok {
		playlist = filepath.Join(s.dir, v+"_index.m3u8")
	} else if name == "master.m3u8" {
		playlist = filepath.Join(s.dir, "v0_index.m3u8")
	}
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
		if strings.HasSuffix(name, ".m3u8") {
			if bytes.Contains(pl, []byte("#EXTINF")) {
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
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
