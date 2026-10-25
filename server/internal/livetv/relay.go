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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

const (
	idleShutdown   = 15 * time.Second // relay with no viewers this long → stop
	hlsClientTTL   = 30 * time.Second // HLS viewer gone if it hasn't polled this long
	clientBuffer   = 512              // chunks a raw-TS viewer may lag before being dropped
	headBytes      = 3 << 20          // start of the stream kept for codec probing
	maxPasses      = 3                // times to cycle through a channel's streams before giving up
	hlsSegmentSecs = 2
	hlsMinSegments = 8 // playlist length when time-shift is off
)

// dvrClient is the viewer address the recorder joins relays with.
const dvrClient = "dvr"

var (
	ErrNoStreams = errors.New("this channel has no streams")
	ErrAllBusy   = errors.New("all of this channel's providers are at their connection limit")
)

// upstreamClient has no overall timeout (streams run for hours) but does
// time out connecting and waiting for headers.
var upstreamClient = &http.Client{Transport: &http.Transport{
	Proxy:                 providerProxy,
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

	hw hwHealth

	mu     sync.Mutex
	relays map[int64]*Relay
}

func NewManager(st *store.Store, ffmpeg, ffprobe, dataDir string) *Manager {
	m := &Manager{st: st, ffmpeg: ffmpeg, ffprobe: ffprobe, dataDir: dataDir, relays: map[int64]*Relay{}}
	os.RemoveAll(m.hlsBase())
	if err := SetProxy(st.Setting(store.SettingLiveProxy)); err != nil {
		log.Printf("live tv: ignoring the provider proxy setting: %v", err)
	}
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

// Ping keeps an HLS viewer alive while its player isn't fetching the
// playlist, for example while paused. It reports whether the viewer is still
// connected.
func (m *Manager) Ping(viewerID string) bool {
	for _, r := range m.snapshot() {
		r.mu.Lock()
		v := r.viewers[viewerID]
		r.mu.Unlock()
		if v != nil {
			v.lastSeen.Store(time.Now().Unix())
			return true
		}
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

// reclaimIdle stops one relay that is pulling from the source but has no
// viewers left (someone flipped away or a guide preview moved on), so its
// connection can be used by a channel someone wants to watch now. Without
// this, flipping channels on a provider with a connection limit fails until
// the idle relay times out.
func (m *Manager) reclaimIdle(sourceID int64, except *Relay) bool {
	m.mu.Lock()
	var idle *Relay
	for _, r := range m.relays {
		if r == except || r.stopped.Load() || r.sourceID.Load() != sourceID {
			continue
		}
		r.mu.Lock()
		empty := len(r.viewers) == 0
		r.mu.Unlock()
		if empty {
			idle = r
			break
		}
	}
	if idle != nil {
		idle.stop("its connection was needed for another channel")
	}
	m.mu.Unlock()
	if idle == nil {
		return false
	}
	// Give the provider a moment to see the old connection close.
	sleepCtx(except.ctx, 500*time.Millisecond)
	return true
}

// preemptForRecording stops the relay on the source with the fewest viewers
// that isn't recording, so a recording can have its connection. It only acts
// when the admin lets recordings take priority over live viewers.
func (m *Manager) preemptForRecording(sourceID int64, except *Relay) bool {
	if m.st.Setting(store.SettingDVRPriority) != "true" || !except.recording() {
		return false
	}
	var victim *Relay
	fewest := 0
	for _, r := range m.snapshot() {
		if r == except || r.stopped.Load() || r.sourceID.Load() != sourceID || r.recording() {
			continue
		}
		r.mu.Lock()
		n := len(r.viewers)
		r.mu.Unlock()
		if victim == nil || n < fewest {
			victim, fewest = r, n
		}
	}
	if victim == nil {
		return false
	}
	victim.stop(fmt.Sprintf("its connection was needed to record %q", except.channel.Name))
	sleepCtx(except.ctx, 500*time.Millisecond)
	return true
}

// ChannelError is the last upstream error of a channel's relay, or def.
func (m *Manager) ChannelError(channelID int64, def string) string {
	if r := m.relay(channelID); r != nil {
		return r.lastErrorOr(def)
	}
	return def
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
	pace      *pace                   // how the current upstream keeps up with real time
	skip      context.CancelCauseFunc // ends the current upstream so run moves to the next stream
	switched  time.Time               // when the last slow-stream switch happened

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

// recording reports whether the DVR is one of the relay's viewers.
func (r *Relay) recording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.viewers {
		if v.IP == dvrClient {
			return true
		}
	}
	return false
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
			if s.MaxStreams > 0 && r.m.sourceInUse(s.SourceID, r) >= s.MaxStreams &&
				!r.m.reclaimIdle(s.SourceID, r) && !r.m.preemptForRecording(s.SourceID, r) {
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
			// Someone may be just leaving another channel; wait a pass
			// before telling the viewer every connection is taken.
			r.mu.Lock()
			r.lastError = ErrAllBusy.Error()
			r.mu.Unlock()
			if pass == maxPasses-1 {
				return
			}
		}
		select {
		case <-r.ctx.Done():
			return
		case <-time.After(time.Duration(pass+1) * 2 * time.Second):
		}
	}
}

// pull copies one upstream into the relay until it errors, stalls, falls
// too far behind real time, or the relay stops.
func (r *Relay) pull(s store.ChannelStream) error {
	ctx, cancel := context.WithCancelCause(r.ctx)
	defer cancel(nil)
	pc := newPace()
	r.mu.Lock()
	r.pace, r.skip = pc, cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.pace, r.skip = nil, nil
		r.mu.Unlock()
	}()
	body, err := r.m.openStream(ctx, s.URL, s.UserAgent)
	if err != nil {
		return err
	}
	defer body.Close()

	// Watchdog: move on when no data arrives for the stall timeout, or when
	// the stream plays slower than real time for that long (players would
	// be buffering) and the channel has another stream to try.
	stall := r.m.stallTimeout()
	slowOK := len(r.channel.Streams) < 2
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
				if time.Since(time.Unix(0, last.Load())) > stall {
					cancel(fmt.Errorf("no data for %s", stall))
					return
				}
				if behind, ok := pc.behind(); ok && !slowOK && behind > stall.Seconds() {
					cancel(fmt.Errorf("too slow: %.0fs behind real time", behind))
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
			pc.feed(buf[:n])
			r.broadcast(buf[:n])
		}
		if err != nil {
			if ctx.Err() != nil && r.ctx.Err() == nil {
				return context.Cause(ctx)
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
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, ffmpegProxyArgs()...)
	args = append(args, "-user_agent", userAgent, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", url, "-map", "0:v?", "-map", "0:a?", "-c", "copy", "-f", "mpegts", "pipe:1")
	cmd := exec.CommandContext(ctx, m.ffmpeg, args...)
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
	Video      string       `json:"video,omitempty"`   // copy | transcode, once HLS runs
	Encoder    string       `json:"encoder,omitempty"` // when transcoding: none (CPU) | nvenc | qsv | vaapi | amf | videotoolbox
	HWError    string       `json:"hwError,omitempty"` // why the configured hardware encoder isn't being used
	Failovers  int          `json:"failovers"`
	LastError  string       `json:"lastError,omitempty"`
	Behind     float64      `json:"behind,omitempty"` // seconds the current stream is behind real time
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
	if r.pace != nil {
		if b, ok := r.pace.behind(); ok && b >= 1 {
			st.Behind = b
		}
	}
	if r.hls != nil {
		st.Video = r.hls.video
		if st.Video == "transcode" {
			st.Encoder = r.hls.encoder
			if want := r.m.st.Setting(store.SettingHWEncoder); want != "none" && want != st.Encoder {
				st.HWError = r.m.hw.lastFailure(want)
			}
		}
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
	dir     string
	video   string // copy | transcode
	encoder string // when transcoding: the encoder in use
	in      chan []byte
	done    chan struct{} // closed when ffmpeg has exited (or never started)
	once    sync.Once

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
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
		var videoArgs []string
		if h.video == "copy" {
			videoArgs = []string{"-c:v", "copy"}
		} else {
			h.encoder = r.m.hw.pick(r.m.st.Setting(store.SettingHWEncoder))
			var in []string
			in, videoArgs = liveEncoder(h.encoder)
			args = append(args, in...)
		}
		args = append(args, "-fflags", "+genpts+discardcorrupt", "-analyzeduration", "3000000", "-probesize", "3000000",
			"-f", "mpegts", "-i", "pipe:0", "-map", "0:v:0?", "-map", "0:a:0?")
		args = append(args, videoArgs...)
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k",
			"-f", "hls", "-hls_time", fmt.Sprint(hlsSegmentSecs), "-hls_list_size", fmt.Sprint(r.m.playlistSegments()),
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
		started := time.Now()
		err = cmd.Wait()
		close(h.done)
		stdin.Close()
		if err != nil && !r.stopped.Load() {
			msg := strings.TrimSpace(stderr.String())
			log.Printf("live tv: channel %q HLS output ended: %v %s", r.channel.Name, err, msg)
			if h.encoder != "" && h.encoder != "none" && time.Since(started) < 20*time.Second {
				// A hardware encoder that dies right away will keep dying;
				// the next start converts on the CPU instead.
				if msg == "" {
					msg = err.Error()
				}
				r.m.hw.markFailed(h.encoder, lastLines(msg, 4))
			}
		}
		// Let the next playlist request start a fresh output if viewers remain.
		r.mu.Lock()
		if r.hls == h {
			r.hls = nil
		}
		r.mu.Unlock()
	}()
}

// liveFilter deinterlaces while converting: broadcast feeds are often
// interlaced MPEG-2.
const liveFilter = "yadif=mode=send_frame:deint=interlaced,format=yuv420p"

// liveEncoder returns the ffmpeg arguments for converting a channel's video,
// split into those that go before the input and those after it.
func liveEncoder(hw string) (in, out []string) {
	in, out = encodeArgs(hw, liveFilter, "6M", "8M")
	g := []string{"-g", fmt.Sprint(hlsSegmentSecs * 30), "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", hlsSegmentSecs)}
	return in, append(out, g...)
}

// stallTimeout is how long a stream may send nothing before the relay
// switches to the channel's next stream.
func (m *Manager) stallTimeout() time.Duration {
	secs, err := strconv.Atoi(m.st.Setting(store.SettingLiveStallSeconds))
	if err != nil || secs < 3 {
		secs = 12
	}
	return time.Duration(secs) * time.Second
}

// StallSeconds is the stall timeout, for players deciding when to report
// that they've been buffering too long.
func (m *Manager) StallSeconds() int { return int(m.stallTimeout().Seconds()) }

// BufferSeconds is how far behind live players sit, so hiccups upstream eat
// into that cushion instead of stalling the picture.
func (m *Manager) BufferSeconds() int {
	secs, err := strconv.Atoi(m.st.Setting(store.SettingLiveBufferSeconds))
	if err != nil || secs < 2 {
		return 10
	}
	return secs
}

// playlistSegments is how many segments each channel's live playlist keeps:
// the time-shift window viewers can pause into and rewind through.
func (m *Manager) playlistSegments() int {
	mins, err := strconv.Atoi(m.st.Setting(store.SettingLiveTimeshift))
	if err != nil || mins <= 0 {
		return hlsMinSegments
	}
	return max(hlsMinSegments, mins*60/hlsSegmentSecs)
}

// probeVideo runs ffprobe over the buffered start of the stream. It tries
// early with a little data (enough for most streams, and it gets the first
// picture out sooner) and again with more if that wasn't enough.
func (r *Relay) probeVideo() string {
	deadline := time.Now().Add(8 * time.Second)
	for _, want := range []int{256 << 10, 1 << 20} {
		for time.Now().Before(deadline) {
			r.mu.Lock()
			n := r.head.Len()
			r.mu.Unlock()
			if n >= want || r.stopped.Load() {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		r.mu.Lock()
		head := append([]byte(nil), r.head.Bytes()...)
		r.mu.Unlock()
		if len(head) == 0 || r.stopped.Load() {
			return ""
		}
		if codec := r.ffprobeCodec(head); codec != "" || !time.Now().Before(deadline) {
			return codec
		}
	}
	return ""
}

func (r *Relay) ffprobeCodec(head []byte) string {
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

// ReportBuffering is a player saying it has been buffering for secs. When
// that passes the stall timeout and the channel has another stream, the relay
// moves to it, but only if the server sees the problem too (the upstream is
// behind real time, or the HLS output has stopped producing segments): a
// viewer on bad Wi-Fi shouldn't switch the stream for everyone else.
func (m *Manager) ReportBuffering(viewerID string, secs float64) (bool, string) {
	var r *Relay
	for _, rr := range m.snapshot() {
		rr.mu.Lock()
		_, ok := rr.viewers[viewerID]
		rr.mu.Unlock()
		if ok {
			r = rr
			break
		}
	}
	if r == nil {
		return false, "not watching"
	}
	stall := m.stallTimeout()
	if secs < stall.Seconds() {
		return false, "not long enough"
	}
	if len(r.channel.Streams) < 2 {
		return false, "no other stream"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.skip == nil {
		return false, "already switching"
	}
	if time.Since(r.switched) < stall {
		return false, "just switched"
	}
	behind, measured := 0.0, false
	if r.pace != nil {
		behind, measured = r.pace.behind()
	}
	segAge := time.Duration(0)
	if r.hls != nil {
		if st, err := os.Stat(filepath.Join(r.hls.dir, "live.m3u8")); err == nil {
			segAge = time.Since(st.ModTime())
		}
	}
	var why string
	switch {
	case measured && behind >= 3:
		why = fmt.Sprintf("viewers buffering; the stream is %.0fs behind real time", behind)
	case segAge > 3*hlsSegmentSecs*time.Second+2*time.Second:
		why = fmt.Sprintf("viewers buffering; no new video for %.0fs", segAge.Seconds())
	case !measured:
		why = "viewers buffering" // can't measure this stream; trust the player
	default:
		log.Printf("live tv: channel %v %q: a viewer reports %.0fs of buffering, but the stream is keeping up (likely the viewer's network)",
			r.channel.Number, r.channel.Name, secs)
		return false, "stream is keeping up"
	}
	r.switched = time.Now()
	r.skip(errors.New(why))
	return true, why
}
