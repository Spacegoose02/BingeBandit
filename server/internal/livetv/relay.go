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
	goodRun        = 30 * time.Second // a stream that played this long resets the pass count
	benchFor       = 10 * time.Minute // a stream left for being slow is tried last for this long
	quietFor       = 5 * time.Second  // a steady stream quiet this long (beyond its usual gaps) is reconnected
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
	ready    bool        // ts viewers: has been sent the stream from a keyframe (guarded by the relay's mu)
	joinedAt int64       // the relay's byte offset when the viewer joined
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

// KickUser disconnects every viewer belonging to a user or its profiles,
// other than the DVR recording for them.
func (m *Manager) KickUser(userID int64) {
	users := map[int64]bool{userID: true}
	if profiles, err := m.st.Household(userID); err == nil {
		for _, p := range profiles {
			users[p.ID] = true
		}
	}
	for _, r := range m.snapshot() {
		r.mu.Lock()
		for id, v := range r.viewers {
			if users[v.UserID] && v.IP != dvrClient {
				delete(r.viewers, id)
				v.close()
			}
		}
		r.mu.Unlock()
	}
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
	gop       *gopCache    // the current upstream from its latest keyframe, for joining ts viewers
	pend      bytes.Buffer // an HLS upstream's data waiting to go out evenly to ts viewers
	headFrom  int64        // where head starts in the relay's byte stream
	off       int64        // bytes relayed so far
	hls       *hlsOutput
	hlsDir    string // kept across output restarts so players' playlist carries on
	hlsBreaks []int  // segment numbers where a new upstream (new timestamps) begins
	benched   map[string]time.Time
	pace      *pace                   // how the current upstream keeps up with real time
	skip      context.CancelCauseFunc // ends the current upstream so run moves to the next stream
	switched  time.Time               // when the last slow-stream switch happened

	sourceID atomic.Int64
	target   atomic.Int64 // the current HLS upstream's segment length (ns), 0 for plain TS
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
	return &Relay{m: m, channel: ch, ctx: ctx, cancel: cancel, started: time.Now(), viewers: map[string]*Viewer{}, rateAt: time.Now(), gop: newGOPCache()}
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
	v.joinedAt = r.off
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
	h, dir := r.hls, r.hlsDir
	r.hls = nil
	r.mu.Unlock()
	if h != nil {
		h.close(true)
	} else if dir != "" {
		go func() {
			time.Sleep(2 * time.Second) // an output being replaced may still be exiting
			os.RemoveAll(dir)
		}()
	}
}

// run tries the channel's streams in priority order, falling back on errors
// and stalls, and skipping providers that are at their connection limit.
func (r *Relay) run() {
	defer r.stop("stream ended")
	go r.smooth()
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
	pulls := 0
	for pass := 0; pass < maxPasses && r.ctx.Err() == nil; pass++ {
		busy := 0
		streams := r.passOrder()
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
			r.resetHead()
			r.mu.Unlock()
			r.sourceID.Store(s.SourceID)
			log.Printf("live tv: channel %v %q ← %q (%s)", r.channel.Number, r.channel.Name, s.Name, s.SourceName)
			if pulls > 0 {
				// The new stream has its own timestamps and may be seconds
				// apart from the last in what it shows; a fresh HLS output
				// marks the change so players carry on cleanly instead of
				// stalling on timestamps that went backwards.
				r.restartHLS()
			}
			pulls++
			began := time.Now()
			err := r.pull(s)
			// A stream that went quiet after playing is usually the
			// provider hiccuping: connecting again right away gets it back
			// within the players' cushion, sooner than waiting out the stall
			// timeout and moving to a stream that may be behind this one.
			for retry := 0; retry < 2 && r.ctx.Err() == nil; retry++ {
				var q quietError
				if !errors.As(err, &q) {
					break
				}
				r.failures.Add(1)
				log.Printf("live tv: channel %v %q: stream %q %v; reconnecting", r.channel.Number, r.channel.Name, s.Name, err)
				r.restartHLSSame()
				err = r.pull(s)
			}
			r.sourceID.Store(0)
			if r.ctx.Err() != nil {
				return
			}
			if time.Since(began) > goodRun {
				pass = -1 // it played fine for a while: this isn't a channel that never works
			}
			var slow slowError
			if errors.As(err, &slow) {
				r.bench(s)
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

// slowError is a stream left because it couldn't keep up with real time,
// as opposed to one that failed outright.
type slowError struct{ why string }

func (e slowError) Error() string { return e.why }

// quietError is a stream that stopped sending for a few seconds after it
// had been keeping up, worth connecting to again before giving up on it.
type quietError struct{ secs float64 }

func (e quietError) Error() string { return fmt.Sprintf("went quiet for %.0fs", e.secs) }

// bench puts a stream that was too slow at the back of the queue for a while,
// so the channel doesn't bounce back to it (and replay what viewers already
// saw from the other stream).
func (r *Relay) bench(s store.ChannelStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.benched == nil {
		r.benched = map[string]time.Time{}
	}
	r.benched[s.URL] = time.Now()
}

func (r *Relay) isBenched(s store.ChannelStream) bool {
	at, ok := r.benched[s.URL]
	return ok && time.Since(at) < benchFor
}

// passOrder is the channel's streams in priority order, with benched ones last.
func (r *Relay) passOrder() []store.ChannelStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]store.ChannelStream, 0, len(r.channel.Streams))
	var later []store.ChannelStream
	for _, s := range r.channel.Streams {
		if r.isBenched(s) {
			later = append(later, s)
		} else {
			out = append(out, s)
		}
	}
	return append(out, later...)
}

// canLeaveSlow reports whether there's somewhere better to go than the
// current, slow stream: another stream that hasn't itself just been left for
// being slow. Without one, staying put beats cycling between streams that
// are each behind the other. switchSlow is the admin's setting, read before
// taking r.mu so a busy database never holds up the relay. Called with r.mu
// held.
func (r *Relay) canLeaveSlow(switchSlow bool) bool {
	if !switchSlow {
		return false
	}
	for _, s := range r.channel.Streams {
		if s.URL != r.stream.URL && !r.isBenched(s) {
			return true
		}
	}
	return false
}

// slowLimit is how far an upstream may fall behind real time before the
// relay leaves it: the players' cushion plus the switch time, so viewers
// would really have been buffering for the switch time.
func (m *Manager) slowLimit() float64 {
	return float64(m.BufferSeconds()) + m.stallTimeout().Seconds()
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
	limit := r.m.slowLimit()
	// An HLS upstream arrives a segment at a time, with a gap about as long
	// as a segment (more when the provider is late updating its playlist)
	// between each. That's normal, not a stall: ffmpeg starts a few segments
	// back, so players have that much in hand.
	quietMin := quietFor
	r.target.Store(0)
	if p, ok := body.(*procReader); ok && p.target > 0 {
		r.target.Store(int64(p.target))
		stall = max(stall, 2*p.target+stall/2)
		limit += p.target.Seconds()
		quietMin = stall // the stall timeout covers it
	}
	var last, maxGap atomic.Int64 // last data; the longest gap between data so far
	var flowing atomic.Bool
	last.Store(time.Now().UnixNano())
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				quiet := time.Since(time.Unix(0, last.Load()))
				if quiet > stall {
					cancel(fmt.Errorf("no data for %s", stall))
					return
				}
				// Quiet for longer than this stream ever pauses: it has
				// stopped, not just finished a burst. (An HLS upstream
				// arrives a segment at a time, with gaps as long as its
				// segments, so the limit grows with the gaps it has had.)
				if flowing.Load() && quiet >= quietMin && quiet >= time.Duration(maxGap.Load())+2*time.Second {
					cancel(quietError{quiet.Seconds()})
					return
				}
				behind, measured := pc.behind()
				if measured && behind > limit {
					switchSlow := r.m.switchSlow()
					r.mu.Lock()
					leave := r.canLeaveSlow(switchSlow) && time.Since(r.switched) > stall
					if leave {
						r.switched = time.Now()
					}
					r.mu.Unlock()
					if leave {
						cancel(slowError{fmt.Sprintf("too slow: %.0fs behind real time", behind)})
						return
					}
				}
			}
		}
	}()

	for {
		buf := make([]byte, 64*1024)
		n, err := body.Read(buf)
		if n > 0 {
			now := time.Now().UnixNano()
			if flowing.Swap(true) {
				if gap := now - last.Load(); gap > maxGap.Load() {
					maxGap.Store(gap)
				}
			}
			last.Store(now)
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
		target := hlsTargetDuration(ctx, resp, userAgent)
		resp.Body.Close()
		rc, err := m.remuxHLS(ctx, final.String(), userAgent)
		if err != nil {
			return nil, err
		}
		rc.target = target
		return rc, nil
	}
	return resp.Body, nil
}

// remuxHLS runs ffmpeg to follow a live HLS playlist and emit MPEG-TS.
func (m *Manager) remuxHLS(ctx context.Context, url, userAgent string) (*procReader, error) {
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
	target time.Duration // an HLS upstream's segment length, when known
}

// defaultHLSTarget is the segment length assumed for an HLS upstream whose
// playlist doesn't say (IPTV providers commonly use 6 to 10 seconds).
const defaultHLSTarget = 10 * time.Second

var reTargetDuration = regexp.MustCompile(`#EXT-X-TARGETDURATION:\s*(\d+)`)

// hlsTargetDuration reads an HLS upstream's segment length from its playlist
// (the first variant's, for a master playlist).
func hlsTargetDuration(ctx context.Context, resp *http.Response, userAgent string) time.Duration {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if m := reTargetDuration.FindSubmatch(b); m != nil {
		if secs, err := strconv.Atoi(string(m[1])); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	if !bytes.Contains(b, []byte("#EXT-X-STREAM-INF")) {
		return defaultHLSTarget
	}
	var variant string
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "#EXT-X-STREAM-INF") && i+1 < len(lines) {
			variant = strings.TrimSpace(lines[i+1])
			break
		}
	}
	u, err := resp.Request.URL.Parse(variant)
	if variant == "" || err != nil {
		return defaultHLSTarget
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return defaultHLSTarget
	}
	req.Header.Set("User-Agent", userAgent)
	vr, err := upstreamClient.Do(req)
	if err != nil {
		return defaultHLSTarget
	}
	defer vr.Body.Close()
	b, _ = io.ReadAll(io.LimitReader(vr.Body, 1<<20))
	if m := reTargetDuration.FindSubmatch(b); m != nil {
		if secs, err := strconv.Atoi(string(m[1])); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultHLSTarget
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

// resetHead starts collecting a new upstream's first bytes. Called with r.mu held.
func (r *Relay) resetHead() {
	r.head.Reset()
	r.headFrom = r.off
	if r.pend.Len() > 0 {
		r.toViewers(bytes.Clone(r.pend.Bytes())) // the last of the old upstream, before its keyframes are forgotten
		r.pend.Reset()
	}
	r.gop.reset()
}

// smoothTick is how often an HLS upstream's data goes out to ts viewers.
const smoothTick = 100 * time.Millisecond

// smooth sends ts viewers (Plex's tuner, IPTV apps) an HLS upstream evenly
// instead of a segment at a time. A burst of ten seconds of video, then ten
// seconds of nothing, is what a playlist with 10 s segments delivers, and
// Plex's player ran dry in every gap and gave up with a playback error. The
// data goes out a segment's length behind, at the pace it comes in.
func (r *Relay) smooth() {
	t := time.NewTicker(smoothTick)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
		}
		r.mu.Lock()
		if n := smoothShare(r.pend.Len(), time.Duration(r.target.Load())); n > 0 {
			r.toViewers(bytes.Clone(r.pend.Next(n)))
		}
		r.mu.Unlock()
	}
}

// smoothShare is how much of queued bytes to send this tick: the share that
// empties the queue over a segment's length, in whole packets.
func smoothShare(queued int, target time.Duration) int {
	if queued == 0 {
		return 0
	}
	if target <= 0 {
		return queued
	}
	n := int(float64(queued) * float64(smoothTick) / float64(target))
	return min(queued, (n/tsPacket+1)*tsPacket)
}

func (r *Relay) broadcast(chunk []byte) {
	r.bytesIn.Add(int64(len(chunk)))
	r.mu.Lock()
	if r.head.Len() < headBytes && r.headFrom+int64(r.head.Len()) == r.off {
		r.head.Write(chunk)
	}
	if r.hls != nil {
		// Fed under the lock so the output can tell which chunks its head
		// start already holds.
		r.hls.feed(tsChunk{off: r.off, b: chunk})
	}
	r.off += int64(len(chunk))
	if r.target.Load() > 0 {
		r.pend.Write(chunk)
	} else {
		r.toViewers(chunk)
	}
	r.mu.Unlock()
}

// toViewers sends relayed data to the ts viewers. Called with r.mu held.
func (r *Relay) toViewers(chunk []byte) {
	r.gop.feed(chunk)
	for id, v := range r.viewers {
		if v.ch == nil {
			continue
		}
		send := chunk
		if !v.ready {
			// Start a new viewer at the latest keyframe (which includes
			// this chunk). A stream whose keyframes can't be found is
			// sent as it comes after a short wait.
			if start := r.gop.start(); start != nil {
				send = start
			} else if r.gop.worthWaiting(r.off-v.joinedAt, time.Since(time.Unix(v.Connected, 0))) {
				continue
			}
			v.ready = true
		}
		select {
		case v.ch <- send:
		default:
			// The viewer can't keep up; drop it rather than stall everyone.
			delete(r.viewers, id)
			v.close()
		}
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
	wantHW := r.m.st.Setting(store.SettingHWEncoder)
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
			if wantHW != "none" && wantHW != st.Encoder {
				st.HWError = r.m.hw.lastFailure(wantHW)
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
	in      chan tsChunk
	from    int64         // the relay's byte offset when the output was created
	done    chan struct{} // closed when ffmpeg has exited (or never started)
	once    sync.Once

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
}

// tsChunk is relayed data and where it sits in the relay's byte stream.
type tsChunk struct {
	off int64
	b   []byte
}

func (h *hlsOutput) feed(chunk tsChunk) {
	select {
	case h.in <- chunk:
	case <-h.done:
	default:
		// ffmpeg is behind; dropping a chunk causes a glitch, not a stall.
	}
}

// close stops the output, and deletes its files when remove is set (the
// channel stopped, rather than a new output taking over the folder).
func (h *hlsOutput) close(remove bool) {
	h.once.Do(func() {
		h.mu.Lock()
		h.stopped = true // stops a start that is still probing
		if h.cmd != nil && h.cmd.Process != nil {
			h.cmd.Process.Kill()
		}
		h.mu.Unlock()
		if !remove {
			return
		}
		go func() {
			<-h.done
			for i := 0; i < 10 && os.RemoveAll(h.dir) != nil; i++ {
				time.Sleep(500 * time.Millisecond)
			}
		}()
	})
}

func (h *hlsOutput) isStopped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

// restartHLS replaces the channel's HLS output, if one is running, with a
// new one that carries on the same playlist after a discontinuity.
func (r *Relay) restartHLS() { r.restartHLSWith(false) }

// restartHLSSame is restartHLS for a reconnect to the same stream: its video
// is known, so the new output starts at once instead of probing it again.
func (r *Relay) restartHLSSame() { r.restartHLSWith(true) }

func (r *Relay) restartHLSWith(sameVideo bool) {
	r.mu.Lock()
	h := r.hls
	if h == nil {
		r.mu.Unlock()
		return
	}
	r.hls = nil
	r.mu.Unlock()
	h.close(false)
	r.ensureHLSAfter(h, sameVideo)
}

// ensureHLS starts the channel's HLS output if it isn't running.
func (r *Relay) ensureHLS() { r.ensureHLSAfter(nil, false) }

// ensureHLSAfter starts the HLS output once prev (an output being replaced)
// has exited. Every output of a relay writes to the same folder and playlist,
// so players keep fetching the same URLs across upstream switches. With
// sameVideo, the new output carries on prev's choice of copying or converting.
func (r *Relay) ensureHLSAfter(prev *hlsOutput, sameVideo bool) {
	r.mu.Lock()
	if r.hls != nil || r.stopped.Load() {
		r.mu.Unlock()
		return
	}
	if r.hlsDir == "" {
		r.hlsDir = filepath.Join(r.m.hlsBase(), fmt.Sprintf("ch%d-%s", r.channel.ID, newID()[:6]))
	}
	dir := r.hlsDir
	h := &hlsOutput{dir: dir, in: make(chan tsChunk, 1024), from: r.off, done: make(chan struct{}), video: "copy"}
	r.hls = h
	r.mu.Unlock()

	go func() {
		if prev != nil {
			select {
			case <-prev.done:
			case <-time.After(5 * time.Second):
			}
		}
		// Wait briefly for stream data so we can tell whether the browser can
		// take the video as-is (H.264) or it needs converting.
		if sameVideo && prev != nil {
			h.video = prev.video
		} else if codec := r.probeVideo(); codec != "" && codec != "h264" {
			h.video = "transcode"
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("live tv: %v", err)
			close(h.done)
			return
		}
		// Carrying on an earlier output's playlist: its next segment starts
		// new timestamps, which players must be told about.
		if last := lastSegment(filepath.Join(dir, "live.m3u8")); last >= 0 {
			r.mu.Lock()
			r.hlsBreaks = append(r.hlsBreaks, last+1)
			r.mu.Unlock()
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
			"-hls_flags", "delete_segments+independent_segments+omit_endlist+append_list",
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
			// Prime ffmpeg with what we already have so the first segment
			// comes quickly, if it runs straight on into what was queued
			// since (an old head would replay the start of the stream).
			r.mu.Lock()
			head := append([]byte(nil), r.head.Bytes()...)
			headEnd := r.headFrom + int64(len(head))
			if h.from < r.headFrom || h.from > headEnd {
				head, headEnd = nil, 0
			}
			r.mu.Unlock()
			stdin.Write(head)
			for {
				select {
				case c := <-h.in:
					b := c.b
					if skip := headEnd - c.off; skip > 0 {
						if skip >= int64(len(b)) {
							continue // already sent as part of the head
						}
						b = b[skip:]
					}
					if _, err := stdin.Write(b); err != nil {
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
		if err != nil && !r.stopped.Load() && !h.isStopped() {
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

// switchSlow is whether the admin lets the relay leave a stream that falls
// behind real time.
func (m *Manager) switchSlow() bool { return m.st.Setting(store.SettingLiveSwitchSlow) != "false" }

// ChannelBufferSeconds is how far behind live players of a channel should
// sit: the usual cushion, plus a segment's worth when the channel's upstream
// is HLS, which arrives a segment at a time with quiet in between.
func (m *Manager) ChannelBufferSeconds(channelID int64) int {
	secs := m.BufferSeconds()
	if r := m.relay(channelID); r != nil {
		secs += int(time.Duration(r.target.Load()).Seconds())
	}
	return secs
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
	switchSlow, buffer := m.switchSlow(), float64(m.BufferSeconds())
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.canLeaveSlow(switchSlow) {
		return false, "no other stream to switch to"
	}
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
	case measured && behind >= max(3, buffer/2):
		why = fmt.Sprintf("viewers buffering; the stream is %.0fs behind real time", behind)
	case segAge > 3*hlsSegmentSecs*time.Second+2*time.Second+time.Duration(r.target.Load()):
		why = fmt.Sprintf("viewers buffering; no new video for %.0fs", segAge.Seconds())
	default:
		// One player buffering while the stream keeps up is that player's
		// network; switching would make everyone else replay what they saw.
		log.Printf("live tv: channel %v %q: a viewer reports %.0fs of buffering, but the stream is keeping up (likely the viewer's network)",
			r.channel.Number, r.channel.Name, secs)
		return false, "stream is keeping up"
	}
	r.switched = time.Now()
	r.skip(slowError{why})
	return true, why
}
