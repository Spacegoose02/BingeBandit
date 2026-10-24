package livetv

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

const (
	stallTimeout   = 12 * time.Second // no bytes from upstream this long → fall back
	idleShutdown   = 15 * time.Second // relay with no viewers this long → stop
	hlsClientTTL   = 30 * time.Second // HLS viewer gone if it hasn't polled this long
	clientBuffer   = 512              // chunks a raw-TS viewer may lag before being dropped
	headBytes      = 3 << 20          // start of the stream kept for codec probing
	maxPasses      = 3                // times to cycle through a channel's streams before giving up
	hlsSegmentSecs = 2
)

var (
	ErrNoStreams = errors.New("this channel has no streams")
	ErrAllBusy   = errors.New("all of this channel's providers are at their connection limit")
)

// upstreamClient has no overall timeout (streams run for hours) but does
// time out connecting and waiting for headers.
var upstreamClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	ResponseHeaderTimeout: 15 * time.Second,
	MaxIdleConnsPerHost:   4,
}}

// Viewer is one person watching through a relay.
type Viewer struct {
	ID        string `json:"id"`
	UserID    int64  `json:"userId"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	Kind      string `json:"kind"` // hls (web player) | ts (external app)
	Connected int64  `json:"connected"`

	lastSeen atomic.Int64
	ch       chan []byte // ts viewers only
	closed   chan struct{}
	once     sync.Once
}

func (v *Viewer) close() { v.once.Do(func() { close(v.closed) }) }

// Done is closed when the relay drops the viewer (stream ended, kicked, too slow).
func (v *Viewer) Done() <-chan struct{} { return v.closed }

// Chunks delivers stream data to a ts viewer.
func (v *Viewer) Chunks() <-chan []byte { return v.ch }

// Manager owns one relay per channel being watched.
type Manager struct {
	st      *store.Store
	ffmpeg  string
	ffprobe string
	dataDir string

	mu     sync.Mutex
	relays map[int64]*Relay
}

func NewManager(st *store.Store, ffmpeg, ffprobe, dataDir string) *Manager {
	m := &Manager{st: st, ffmpeg: ffmpeg, ffprobe: ffprobe, dataDir: dataDir, relays: map[int64]*Relay{}}
	os.RemoveAll(m.hlsBase())
	go m.janitor()
	return m
}

func (m *Manager) hlsBase() string {
	if d := strings.TrimSpace(m.st.Setting(store.SettingTranscodeDir)); d != "" {
		return filepath.Join(d, "live")
	}
	return filepath.Join(m.dataDir, "transcode", "live")
}

func newID() string {
	b := make([]byte, 10)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Join adds a viewer to the channel's relay, starting it if needed.
func (m *Manager) Join(ch *store.Channel, user *store.User, ip, kind string) (*Viewer, error) {
	if len(ch.Streams) == 0 && !ch.Custom {
		return nil, ErrNoStreams
	}
	v := &Viewer{ID: newID(), UserID: user.ID, Username: user.Username, IP: ip, Kind: kind,
		Connected: time.Now().Unix(), closed: make(chan struct{})}
	v.lastSeen.Store(time.Now().Unix())
	if kind == "ts" {
		v.ch = make(chan []byte, clientBuffer)
	}
	m.mu.Lock()
	r := m.relays[ch.ID]
	if r == nil || r.stopped.Load() {
		r = m.newRelay(ch)
		m.relays[ch.ID] = r
		go r.run()
	}
	r.add(v)
	m.mu.Unlock()
	if kind == "hls" {
		r.ensureHLS()
	}
	return v, nil
}

// Leave removes a viewer by id from whichever relay has it.
func (m *Manager) Leave(viewerID string) {
	for _, r := range m.snapshot() {
		r.remove(viewerID)
	}
}

// Touch keeps an HLS viewer alive (called on every playlist fetch).
func (m *Manager) Touch(channelID int64, viewerID string) bool {
	r := m.relay(channelID)
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if v := r.viewers[viewerID]; v != nil {
		v.lastSeen.Store(time.Now().Unix())
		return true
	}
	return false
}

func (m *Manager) relay(channelID int64) *Relay {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.relays[channelID]
}

func (m *Manager) snapshot() []*Relay {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Relay, 0, len(m.relays))
	for _, r := range m.relays {
		out = append(out, r)
	}
	return out
}

// Stop ends a channel's relay and disconnects its viewers.
func (m *Manager) Stop(channelID int64) {
	if r := m.relay(channelID); r != nil {
		r.stop("stopped by an admin")
	}
}

// sourceInUse counts relays currently pulling from a source.
func (m *Manager) sourceInUse(sourceID int64, except *Relay) int {
	n := 0
	for _, r := range m.snapshot() {
		if r != except && !r.stopped.Load() && r.sourceID.Load() == sourceID {
			n++
		}
	}
	return n
}

func (m *Manager) janitor() {
	for range time.Tick(5 * time.Second) {
		now := time.Now().Unix()
		for _, r := range m.snapshot() {
			r.mu.Lock()
			for id, v := range r.viewers {
				if v.Kind == "hls" && now-v.lastSeen.Load() > int64(hlsClientTTL.Seconds()) {
					delete(r.viewers, id)
					v.close()
				}
			}
			empty := len(r.viewers) == 0
			if !empty {
				r.idleSince = time.Time{}
			} else if r.idleSince.IsZero() {
				r.idleSince = time.Now()
			}
			idle := empty && time.Since(r.idleSince) > idleShutdown
			r.mu.Unlock()
			if idle {
				r.stop("no viewers")
			}
			if r.stopped.Load() {
				m.mu.Lock()
				if m.relays[r.channel.ID] == r {
					delete(m.relays, r.channel.ID)
				}
				m.mu.Unlock()
			}
		}
	}
}

// Relay pulls one channel from upstream and fans it out to every viewer.
type Relay struct {
	m       *Manager
	channel *store.Channel
	ctx     context.Context
	cancel  context.CancelFunc
	started time.Time

	mu        sync.Mutex
	viewers   map[string]*Viewer
	idleSince time.Time
	stream    store.ChannelStream
	lastError string
	head      bytes.Buffer // first bytes of the current upstream, for probing
	hls       *hlsOutput

	sourceID atomic.Int64
	bytesIn  atomic.Int64
	stopped  atomic.Bool
	failures atomic.Int32 // upstream switches since start

	rateMu   sync.Mutex
	rateAt   time.Time
	rateFrom int64
	rateBps  float64
}

func (m *Manager) newRelay(ch *store.Channel) *Relay {
	ctx, cancel := context.WithCancel(context.Background())
	return &Relay{m: m, channel: ch, ctx: ctx, cancel: cancel, started: time.Now(), viewers: map[string]*Viewer{}, rateAt: time.Now()}
}

func (r *Relay) add(v *Viewer) {
	r.mu.Lock()
	r.viewers[v.ID] = v
	r.mu.Unlock()
}

func (r *Relay) remove(id string) {
	r.mu.Lock()
	if v := r.viewers[id]; v != nil {
		delete(r.viewers, id)
		v.close()
	}
	r.mu.Unlock()
}

func (r *Relay) stop(reason string) {
	if r.stopped.Swap(true) {
		return
	}
	log.Printf("live tv: channel %v %q stopped (%s)", r.channel.Number, r.channel.Name, reason)
	r.cancel()
	r.mu.Lock()
	for id, v := range r.viewers {
		delete(r.viewers, id)
		v.close()
	}
	h := r.hls
	r.hls = nil
	r.mu.Unlock()
	if h != nil {
		h.close()
	}
}

// run tries the channel's streams in priority order, falling back on errors
// and stalls, and skipping providers that are at their connection limit.
func (r *Relay) run() {
	defer r.stop("stream ended")
	if r.channel.Custom {
		for attempt := 0; attempt < maxPasses && r.ctx.Err() == nil; attempt++ {
			err := r.pullCustom()
			if err == nil {
				return
			}
			r.failures.Add(1)
			r.mu.Lock()
			r.lastError = err.Error()
			r.mu.Unlock()
			log.Printf("live tv: custom channel %q: %v", r.channel.Name, err)
			sleepCtx(r.ctx, 3*time.Second)
		}
		return
	}
	streams := r.channel.Streams
	for pass := 0; pass < maxPasses && r.ctx.Err() == nil; pass++ {
		busy := 0
		for _, s := range streams {
			if r.ctx.Err() != nil {
				return
			}
			if s.MaxStreams > 0 && r.m.sourceInUse(s.SourceID, r) >= s.MaxStreams {
				busy++
				continue
			}
			r.mu.Lock()
			r.stream = s
			r.head.Reset()
			r.mu.Unlock()
			r.sourceID.Store(s.SourceID)
			log.Printf("live tv: channel %v %q ← %q (%s)", r.channel.Number, r.channel.Name, s.Name, s.SourceName)
			err := r.pull(s)
			r.sourceID.Store(0)
			if r.ctx.Err() != nil {
				return
			}
			r.failures.Add(1)
			r.mu.Lock()
			r.lastError = fmt.Sprintf("%s: %v", s.Name, err)
			r.mu.Unlock()
			log.Printf("live tv: channel %v %q: stream %q failed: %v; trying the next one", r.channel.Number, r.channel.Name, s.Name, err)
		}
		if busy == len(streams) {
			r.mu.Lock()
			r.lastError = ErrAllBusy.Error()
			r.mu.Unlock()
			return
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Duration(pass+1) * 2 * time.Second):
		}
	}
}

// pull copies one upstream into the relay until it errors, stalls or the relay stops.
func (r *Relay) pull(s store.ChannelStream) error {
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	body, err := r.m.openStream(ctx, s.URL, s.UserAgent)
	if err != nil {
		return err
	}
	defer body.Close()

	// Watchdog: cancel the request when no data arrives for stallTimeout.
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if time.Since(time.Unix(0, last.Load())) > stallTimeout {
					cancel()
					return
				}
			}
		}
	}()

	for {
		buf := make([]byte, 64*1024)
		n, err := body.Read(buf)
		if n > 0 {
			last.Store(time.Now().UnixNano())
			r.broadcast(buf[:n])
		}
		if err != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return fmt.Errorf("no data for %s", stallTimeout)
			}
			if err == io.EOF {
				return errors.New("upstream closed the stream")
			}
			return err
		}
	}
}

// openStream connects to an upstream and returns MPEG-TS. Plain TS streams
// are read directly; HLS upstreams (.m3u8, common with IPTV providers) are
// pulled and remuxed to TS by ffmpeg without re-encoding.
func (m *Manager) openStream(ctx context.Context, url, userAgent string) (io.ReadCloser, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("only http(s) streams are supported")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if userAgent == "" {
		userAgent = "VLC/3.0.20 LibVLC/3.0.20" // what most IPTV providers expect
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := upstreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream answered %s", resp.Status)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	final := resp.Request.URL // after redirects, which providers love
	if strings.Contains(ct, "mpegurl") || strings.HasSuffix(strings.ToLower(final.Path), ".m3u8") {
		resp.Body.Close()
		return m.remuxHLS(ctx, final.String(), userAgent)
	}
	return resp.Body, nil
}

// remuxHLS runs ffmpeg to follow a live HLS playlist and emit MPEG-TS.
func (m *Manager) remuxHLS(ctx context.Context, url, userAgent string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, m.ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin",
		"-user_agent", userAgent, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", url, "-map", "0:v?", "-map", "0:a?", "-c", "copy", "-f", "mpegts", "pipe:1")
	media.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg for HLS upstream: %w", err)
	}
	return &procReader{ReadCloser: out, cmd: cmd, stderr: &stderr}, nil
}

// procReader is ffmpeg's stdout; closing it stops the process.
type procReader struct {
	io.ReadCloser
	cmd    *exec.Cmd
	stderr *bytes.Buffer
}

func (p *procReader) Read(b []byte) (int, error) {
	n, err := p.ReadCloser.Read(b)
	if err == io.EOF {
		if msg := strings.TrimSpace(p.stderr.String()); msg != "" {
			return n, fmt.Errorf("HLS upstream ended: %s", msg)
		}
	}
	return n, err
}

func (p *procReader) Close() error {
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	p.ReadCloser.Close()
	p.cmd.Wait()
	return nil
}

func (r *Relay) broadcast(chunk []byte) {
	r.bytesIn.Add(int64(len(chunk)))
	r.mu.Lock()
	if r.head.Len() < headBytes {
		r.head.Write(chunk)
	}
	h := r.hls
	for id, v := range r.viewers {
		if v.ch == nil {
			continue
		}
		select {
		case v.ch <- chunk:
		default:
			// The viewer can't keep up; drop it rather than stall everyone.
			delete(r.viewers, id)
			v.close()
		}
	}
	r.mu.Unlock()
	if h != nil {
		h.feed(chunk)
	}
}

// Stats for the admin Live TV activity view.
type ViewerStat struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	IP        string `json:"ip"`
	Kind      string `json:"kind"`
	Connected int64  `json:"connected"`
}

type RelayStat struct {
	ChannelID  int64        `json:"channelId"`
	Number     float64      `json:"number"`
	Name       string       `json:"name"`
	Logo       string       `json:"logo,omitempty"`
	EPGID      string       `json:"epgId,omitempty"`
	Stream     string       `json:"stream"`
	Source     string       `json:"source"`
	Started    int64        `json:"started"`
	Bytes      int64        `json:"bytes"`
	AvgBps     float64      `json:"avgBps"`
	CurrentBps float64      `json:"currentBps"`
	Video      string       `json:"video,omitempty"` // copy | transcode, once HLS runs
	Failovers  int          `json:"failovers"`
	LastError  string       `json:"lastError,omitempty"`
	Viewers    []ViewerStat `json:"viewers"`
}

func (r *Relay) stat() RelayStat {
	now := time.Now()
	bytes := r.bytesIn.Load()
	r.rateMu.Lock()
	if dt := now.Sub(r.rateAt).Seconds(); dt >= 2 {
		r.rateBps = float64(bytes-r.rateFrom) * 8 / dt
		r.rateAt, r.rateFrom = now, bytes
	}
	cur := r.rateBps
	r.rateMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	st := RelayStat{
		ChannelID: r.channel.ID, Number: r.channel.Number, Name: r.channel.Name, Logo: r.channel.Logo, EPGID: r.channel.EPGID,
		Stream: r.stream.Name, Source: r.stream.SourceName, Started: r.started.Unix(), Bytes: bytes,
		AvgBps: float64(bytes) * 8 / max(now.Sub(r.started).Seconds(), 1), CurrentBps: cur,
		Failovers: int(r.failures.Load()), LastError: r.lastError, Viewers: []ViewerStat{},
	}
	if r.hls != nil {
		st.Video = r.hls.video
	}
	for _, v := range r.viewers {
		st.Viewers = append(st.Viewers, ViewerStat{ID: v.ID, Username: v.Username, IP: v.IP, Kind: v.Kind, Connected: v.Connected})
	}
	sort.Slice(st.Viewers, func(i, j int) bool { return st.Viewers[i].Connected < st.Viewers[j].Connected })
	return st
}

func (m *Manager) Stats() []RelayStat {
	out := []RelayStat{}
	for _, r := range m.snapshot() {
		if !r.stopped.Load() {
			out = append(out, r.stat())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// ---- HLS output for web players -------------------------------------------

// hlsOutput is one ffmpeg per channel turning the relayed MPEG-TS into a
// rolling HLS playlist that every web viewer of the channel shares.
type hlsOutput struct {
	dir   string
	video string // copy | transcode
	in    chan []byte
	done  chan struct{} // closed when ffmpeg has exited (or never started)
	once  sync.Once

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
}

func (h *hlsOutput) feed(chunk []byte) {
	select {
	case h.in <- chunk:
	case <-h.done:
	default:
		// ffmpeg is behind; dropping a chunk causes a glitch, not a stall.
	}
}

func (h *hlsOutput) close() {
	h.once.Do(func() {
		h.mu.Lock()
		h.stopped = true // stops a start that is still probing
		if h.cmd != nil && h.cmd.Process != nil {
			h.cmd.Process.Kill()
		}
		h.mu.Unlock()
		go func() {
			<-h.done
			for i := 0; i < 10 && os.RemoveAll(h.dir) != nil; i++ {
				time.Sleep(500 * time.Millisecond)
			}
		}()
	})
}

// ensureHLS starts the channel's HLS output if it isn't running.
func (r *Relay) ensureHLS() {
	r.mu.Lock()
	if r.hls != nil || r.stopped.Load() {
		r.mu.Unlock()
		return
	}
	dir := filepath.Join(r.m.hlsBase(), fmt.Sprintf("ch%d-%s", r.channel.ID, newID()[:6]))
	h := &hlsOutput{dir: dir, in: make(chan []byte, 1024), done: make(chan struct{}), video: "copy"}
	r.hls = h
	r.mu.Unlock()

	go func() {
		// Wait briefly for stream data so we can tell whether the browser can
		// take the video as-is (H.264) or it needs converting.
		codec := r.probeVideo()
		if codec != "" && codec != "h264" {
			h.video = "transcode"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("live tv: %v", err)
			close(h.done)
			return
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin",
			"-fflags", "+genpts+discardcorrupt", "-analyzeduration", "3000000", "-probesize", "3000000",
			"-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0?", "-map", "0:a:0?"}
		if h.video == "copy" {
			args = append(args, "-c:v", "copy")
		} else {
			// Broadcast feeds are often interlaced MPEG-2: deinterlace while converting.
			args = append(args, liveEncoder(r.m.st.Setting(store.SettingHWEncoder))...)
		}
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k",
			"-f", "hls", "-hls_time", fmt.Sprint(hlsSegmentSecs), "-hls_list_size", "8",
			"-hls_flags", "delete_segments+independent_segments+omit_endlist",
			"-hls_segment_filename", filepath.Join(dir, "s%06d.ts"), filepath.Join(dir, "live.m3u8"))
		cmd := exec.Command(r.m.ffmpeg, args...)
		media.HideWindow(cmd)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			close(h.done)
			return
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		h.mu.Lock()
		if h.stopped {
			h.mu.Unlock()
			close(h.done)
			return
		}
		if err := cmd.Start(); err != nil {
			h.mu.Unlock()
			log.Printf("live tv: start ffmpeg: %v", err)
			close(h.done)
			return
		}
		h.cmd = cmd
		h.mu.Unlock()
		go func() {
			// Prime ffmpeg with what we already have so the first segment comes quickly.
			r.mu.Lock()
			head := append([]byte(nil), r.head.Bytes()...)
			r.mu.Unlock()
			stdin.Write(head)
			for {
				select {
				case chunk := <-h.in:
					if _, err := stdin.Write(chunk); err != nil {
						return
					}
				case <-h.done:
					return
				}
			}
		}()
		err = cmd.Wait()
		close(h.done)
		stdin.Close()
		if err != nil && !r.stopped.Load() {
			log.Printf("live tv: channel %q HLS output ended: %v %s", r.channel.Name, err, strings.TrimSpace(stderr.String()))
		}
		// Let the next playlist request start a fresh output if viewers remain.
		r.mu.Lock()
		if r.hls == h {
			r.hls = nil
		}
		r.mu.Unlock()
	}()
}

func liveEncoder(hw string) []string {
	vf := []string{"-vf", "yadif=mode=send_frame:deint=interlaced,format=yuv420p"}
	g := []string{"-g", fmt.Sprint(hlsSegmentSecs * 30), "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentSecs)}
	switch hw {
	case "nvenc":
		return append(append(vf, "-c:v", "h264_nvenc", "-preset", "p4", "-b:v", "6M", "-maxrate", "8M", "-bufsize", "12M"), g...)
	case "qsv":
		return append(append([]string{"-vf", "yadif=mode=send_frame:deint=interlaced,format=nv12"}, "-c:v", "h264_qsv", "-preset", "veryfast", "-b:v", "6M"), g...)
	case "amf":
		return append(append(vf, "-c:v", "h264_amf", "-quality", "speed", "-b:v", "6M"), g...)
	default:
		return append(append(vf, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-crf", "22", "-maxrate", "8M", "-bufsize", "12M"), g...)
	}
}

// probeVideo runs ffprobe over the buffered start of the stream.
func (r *Relay) probeVideo() string {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		n := r.head.Len()
		r.mu.Unlock()
		if n >= 1<<20 || r.stopped.Load() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	r.mu.Lock()
	head := append([]byte(nil), r.head.Bytes()...)
	r.mu.Unlock()
	if len(head) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.m.ffprobe, "-v", "error", "-f", "mpegts", "-i", "pipe:0",
		"-select_streams", "v:0", "-show_entries", "stream=codec_name", "-of", "csv=p=0")
	media.HideWindow(cmd)
	cmd.Stdin = bytes.NewReader(head)
	out, _ := cmd.Output()
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

var reHLSFile = regexp.MustCompile(`^(live\.m3u8|s\d{6}\.ts)$`)

// HLSFile returns the path of a channel's playlist or segment, waiting for
// the first segment when the output has just started.
func (m *Manager) HLSFile(ctx context.Context, channelID int64, name string) (string, error) {
	if !reHLSFile.MatchString(name) {
		return "", os.ErrNotExist
	}
	r := m.relay(channelID)
	if r == nil || r.stopped.Load() {
		return "", os.ErrNotExist
	}
	deadline := time.After(25 * time.Second)
	for {
		r.ensureHLS()
		r.mu.Lock()
		h := r.hls
		r.mu.Unlock()
		if h != nil {
			p := filepath.Join(h.dir, name)
			if name == "live.m3u8" {
				if b, err := readShared(p); err == nil && bytes.Contains(b, []byte("#EXTINF")) {
					return p, nil
				}
			} else if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
		if r.stopped.Load() {
			return "", errors.New(r.lastErrorOr("the channel stopped"))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", errors.New(r.lastErrorOr("the channel didn't start in time"))
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (r *Relay) lastErrorOr(def string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastError != "" {
		return r.lastError
	}
	return def
}
