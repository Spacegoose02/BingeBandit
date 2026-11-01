package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/cast"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// Casting: the server plays a title on a Chromecast, Google TV or DLNA TV
// on the home network, and apps act as its remote. The server sends the TV
// a link to the file (or to a conversion of it) carrying a token that only
// opens this title's media and ends with the cast.

const (
	castScanWait   = 2500 * time.Millisecond
	castScanMaxAge = 60 * time.Second
	castPoll       = 2 * time.Second
	castReport     = 10 * time.Second
	// castStartLimit is how long a TV may take to start playing before the
	// cast is given up on.
	castStartLimit = 90 * time.Second
)

// castDiscover and castOpen are replaced in tests.
var (
	castDiscover = cast.Discover
	castOpen     = cast.Open
)

type castManager struct {
	scanMu  sync.Mutex // one scan at a time
	mu      sync.Mutex
	devices []cast.Device
	scanned time.Time
	// sessions by id, and media tokens → session id.
	sessions map[string]*castSession
	tokens   map[string]string
}

// castView is what apps see of a cast.
type castView struct {
	ID       string      `json:"id"`
	Device   cast.Device `json:"device"`
	ItemID   int64       `json:"itemId"`
	FileID   int64       `json:"fileId"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle,omitempty"`
	Poster   string      `json:"poster,omitempty"`
	Duration float64     `json:"duration"`
	Position float64     `json:"position"`
	State    string      `json:"state"`
	Volume   float64     `json:"volume"`
	Mode     string      `json:"mode"` // direct | transcode
	Error    string      `json:"error,omitempty"`
	Started  int64       `json:"started"`
}

type castSession struct {
	castView

	userID      int64
	token       string
	audio       int
	offset      float64 // file time at the start of what the TV was sent
	transcodeID string
	player      cast.Player
	playID      string // for now-playing and history
	played      float64
	seenPlaying bool // since the last load
	everPlayed  bool
	loadedAt    time.Time
	quietUntil  time.Time // a reload is under way: idle doesn't mean stopped
	reported    time.Time
	ended       bool

	mu     sync.Mutex
	cancel context.CancelFunc
}

func (c *castSession) snapshot() castView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.castView
}

func validCastSettings(in map[string]string) error {
	if v, ok := in[store.SettingCastURL]; ok {
		v = strings.TrimRight(v, "/")
		in[store.SettingCastURL] = v
		if v != "" {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
				return errors.New("the casting address must look like http://192.168.1.10:8484")
			}
		}
	}
	if v, ok := in[store.SettingCastDevices]; ok {
		var lines []string
		for _, line := range strings.FieldsFunc(v, func(r rune) bool { return r == '\n' || r == ',' }) {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			host := line
			if h, _, err := net.SplitHostPort(line); err == nil {
				host = h
			}
			if net.ParseIP(host) == nil {
				return fmt.Errorf("%q isn't an IP address (TVs to cast to are listed by address, one per line)", line)
			}
			lines = append(lines, host)
		}
		in[store.SettingCastDevices] = strings.Join(lines, "\n")
	}
	return nil
}

// --- devices ----------------------------------------------------------------

func (s *Server) castDevices(ctx context.Context, refresh bool) []cast.Device {
	m := &s.casts
	m.mu.Lock()
	fresh := !refresh && time.Since(m.scanned) < castScanMaxAge
	devices := m.devices
	m.mu.Unlock()
	if fresh {
		return devices
	}
	m.scanMu.Lock()
	defer m.scanMu.Unlock()
	m.mu.Lock()
	if !refresh && time.Since(m.scanned) < castScanMaxAge {
		devices = m.devices // another request just scanned
		m.mu.Unlock()
		return devices
	}
	m.mu.Unlock()
	extra := strings.Split(s.Store.Setting(store.SettingCastDevices), "\n")
	// Not cut short when the asker gives up: others share the result.
	found := castDiscover(context.WithoutCancel(ctx), castScanWait, extra)
	m.mu.Lock()
	m.devices, m.scanned = found, time.Now()
	m.mu.Unlock()
	return found
}

func (s *Server) castDeviceList(w http.ResponseWriter, r *http.Request) {
	devices := s.castDevices(r.Context(), r.URL.Query().Get("refresh") == "1")
	if devices == nil {
		devices = []cast.Device{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// --- the address TVs reach the server at ------------------------------------

// castBase is the server's address as seen from host: the admin's casting
// address if set, else this machine's address on the way to host and the
// port it listens on.
func (s *Server) castBase(host string) (string, error) {
	if v := s.Store.Setting(store.SettingCastURL); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	conn, err := net.Dial("udp", net.JoinHostPort(host, "9"))
	if err != nil {
		return "", fmt.Errorf("can't find a way to the TV: %w", err)
	}
	ip := conn.LocalAddr().(*net.UDPAddr).IP
	conn.Close()
	port := "8484"
	if _, p, err := net.SplitHostPort(s.ListenAddr); err == nil && p != "" && p != "0" {
		port = p
	}
	return "http://" + net.JoinHostPort(ip.String(), port), nil
}

// castTokenUser is the user a cast media token was made for, when token is
// one and path is media it may open: the cast's file, its conversion, or
// artwork.
func (s *Server) castTokenUser(token, path string) *store.User {
	if token == "" {
		return nil
	}
	m := &s.casts
	m.mu.Lock()
	id, ok := m.tokens[token]
	c := m.sessions[id]
	m.mu.Unlock()
	if !ok || c == nil {
		return nil
	}
	c.mu.Lock()
	fileID, tid, uid := c.FileID, c.transcodeID, c.userID
	c.mu.Unlock()
	allowed := path == fmt.Sprintf("/api/files/%d/direct", fileID) ||
		(tid != "" && strings.HasPrefix(path, "/api/transcode/"+tid+"/")) ||
		strings.HasPrefix(path, "/api/images/")
	if !allowed {
		return nil
	}
	u, err := s.Store.UserByID(uid)
	if err != nil {
		return nil
	}
	return u
}

// --- what to send -----------------------------------------------------------

// castDirect reports whether a TV of kind can play mf as it is, which spares
// the server a conversion. Chromecasts and DLNA TVs reliably play MP4 with
// 8-bit H.264 and AAC or MP3 up to 1080p; anything else is converted.
func castDirect(kind string, mf *store.MediaFile) bool {
	ext := strings.ToLower(filepath.Ext(mf.Path))
	if ext != ".mp4" && ext != ".m4v" {
		return false
	}
	if mf.VideoCodec != "h264" || mf.HDR || mf.BitDepth > 8 || mf.Height > 1080 {
		return false
	}
	switch mf.AudioCodec {
	case "aac", "mp3", "":
	default:
		return false
	}
	// DLNA TVs often can't take AAC 5.1.
	return kind == cast.KindChromecast || mf.AudioChannels <= 2
}

// castMedia picks how c's file goes to the TV from at (file seconds),
// starting a conversion when it has to, and returns what to load.
func (s *Server) castMedia(c *castSession, mf *store.MediaFile, at float64) (cast.Media, error) {
	base, err := s.castBase(c.Device.Host)
	if err != nil {
		return cast.Media{}, err
	}
	m := cast.Media{Title: c.Title, Subtitle: c.Subtitle, Duration: c.Duration}
	if c.Poster != "" {
		p := c.Poster
		if strings.HasPrefix(p, "/api/images/") {
			p = base + p + "?w=w500&token=" + c.token
		}
		m.Image = p
	}
	q := "?token=" + url.QueryEscape(c.token)
	if c.audio < 0 && castDirect(c.Device.Kind, mf) {
		c.mu.Lock()
		old := c.transcodeID
		c.Mode, c.offset, c.transcodeID = "direct", 0, ""
		c.mu.Unlock()
		if old != "" {
			s.Transcoder.Stop(old)
		}
		m.URL, m.ContentType, m.Start = fmt.Sprintf("%s/api/files/%d/direct%s", base, mf.ID, q), "video/mp4", at
		return m, nil
	}
	if s.Transcoder == nil {
		return cast.Media{}, errors.New("this server can't convert video")
	}
	c.mu.Lock()
	replace := c.transcodeID
	c.mu.Unlock()
	sess, err := s.Transcoder.Start(mf, c.userID, stream.Options{
		Start: at, MaxHeight: 1080, AudioStream: c.audio, BurnSubtitle: -1, Replace: replace,
		AudioCodecs: []string{"aac", "mp3"}, MaxAudioChannels: 2,
	})
	if err != nil {
		log.Printf("cast: transcode: %v", err)
		return cast.Media{}, errors.New("could not start converting the video")
	}
	c.mu.Lock()
	c.Mode, c.offset, c.transcodeID = "transcode", at, sess.ID
	c.mu.Unlock()
	m.Duration = 0 // what's sent starts at "at" and grows while it converts
	if c.Device.Kind == cast.KindChromecast {
		m.URL, m.ContentType = fmt.Sprintf("%s/api/transcode/%s/index.m3u8%s", base, sess.ID, q), "application/x-mpegURL"
	} else {
		// Most TVs' DLNA players can't open HLS; the conversion's segments
		// go to them as one continuous MPEG-TS stream instead.
		m.URL, m.ContentType = fmt.Sprintf("%s/api/transcode/%s/stream.ts%s", base, sess.ID, q), "video/mpeg"
	}
	m.Live = true
	return m, nil
}

// --- starting and running a cast --------------------------------------------

type castStartReq struct {
	DeviceID    string  `json:"deviceId"`
	ItemID      int64   `json:"itemId"`
	FileID      int64   `json:"fileId"`
	Start       float64 `json:"start"`
	AudioStream *int    `json:"audioStream"`
}

func (s *Server) castStart(w http.ResponseWriter, r *http.Request) {
	var req castStartReq
	if err := readJSON(r, &req); err != nil || req.DeviceID == "" || req.ItemID == 0 {
		writeErr(w, http.StatusBadRequest, "pick a TV and something to play")
		return
	}
	u := currentUser(r)
	it, err := s.Store.Item(u.ID, req.ItemID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if it.Kind != "movie" && it.Kind != "episode" && it.Kind != "video" {
		writeErr(w, http.StatusBadRequest, "pick a movie or an episode to cast")
		return
	}
	files, err := s.Store.FilesForItem(it.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var mf *store.MediaFile
	for _, f := range files {
		if req.FileID == 0 || f.ID == req.FileID {
			mf = f
			break
		}
	}
	if mf == nil {
		writeErr(w, http.StatusNotFound, "this title has no file to play")
		return
	}
	var dev *cast.Device
	for _, refresh := range []bool{false, true} {
		for _, d := range s.castDevices(r.Context(), refresh) {
			if d.ID == req.DeviceID {
				dev = &d
				break
			}
		}
		if dev != nil {
			break
		}
	}
	if dev == nil {
		writeErr(w, http.StatusNotFound, "that TV can't be found any more; check it's on")
		return
	}
	if msg := s.streamLimitHit(u, dev.Host, mf.ID); msg != "" {
		writeErr(w, http.StatusTooManyRequests, msg)
		return
	}
	// One cast per TV: a new one takes over, as on the TV itself.
	for _, old := range s.castSessionsWhere(func(c *castSession) bool { return c.Device.ID == dev.ID }) {
		s.endCast(old, "")
	}

	c := &castSession{
		castView: castView{
			ID: randHex(8), Device: *dev, ItemID: it.ID, FileID: mf.ID, Title: it.Title, Poster: it.Poster,
			Duration: mf.Duration, State: cast.StateLoading, Volume: -1, Started: time.Now().Unix(),
		},
		userID: u.ID, token: randHex(24), audio: -1, playID: "cast-" + randHex(8),
	}
	if c.Duration == 0 {
		c.Duration = float64(it.Runtime * 60)
	}
	if req.AudioStream != nil {
		c.audio = *req.AudioStream
	}
	if it.Kind == "episode" && it.SeasonNumber != nil && it.EpisodeNumber != nil {
		c.Title = it.ShowTitle
		c.Subtitle = fmt.Sprintf("S%d E%d · %s", *it.SeasonNumber, *it.EpisodeNumber, it.Title)
		if it.ShowPoster != "" {
			c.Poster = it.ShowPoster
		}
	}
	start := req.Start
	if start < 0 || (c.Duration > 0 && start >= c.Duration-5) {
		start = 0
	}
	c.Position = start

	m := &s.casts
	m.mu.Lock()
	if m.sessions == nil {
		m.sessions, m.tokens = map[string]*castSession{}, map[string]string{}
	}
	m.sessions[c.ID], m.tokens[c.token] = c, c.ID
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	player, err := castOpen(ctx, *dev)
	if err == nil {
		c.player = player
		err = s.castLoad(ctx, c, mf, start)
	}
	if err != nil {
		s.endCast(c, "")
		writeErr(w, http.StatusBadGateway, "couldn't play on "+dev.Name+": "+err.Error())
		return
	}
	runCtx, stop := context.WithCancel(s.ctx)
	c.mu.Lock()
	c.cancel = stop
	c.mu.Unlock()
	go s.runCast(runCtx, c)
	writeJSON(w, http.StatusOK, c.snapshot())
}

// castLoad sends the file from at to the TV.
func (s *Server) castLoad(ctx context.Context, c *castSession, mf *store.MediaFile, at float64) error {
	c.mu.Lock()
	c.quietUntil = time.Now().Add(time.Minute)
	c.mu.Unlock()
	media, err := s.castMedia(c, mf, at)
	if err != nil {
		return err
	}
	if c.Mode == "transcode" {
		media.Start = 0
	}
	c.mu.Lock()
	c.loadedAt, c.quietUntil, c.seenPlaying = time.Now(), time.Now().Add(20*time.Second), false
	c.State, c.Position, c.Error = cast.StateLoading, at, ""
	c.mu.Unlock()
	return c.player.Load(ctx, media)
}

// runCast follows the TV until the cast ends: it keeps the position for the
// remote and for Continue Watching, and keeps a paused conversion alive.
func (s *Server) runCast(ctx context.Context, c *castSession) {
	t := time.NewTicker(castPoll)
	defer t.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		st, err := c.player.Status(pctx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if failures++; failures >= 4 {
				s.endCast(c, "Lost touch with "+c.Device.Name+": "+err.Error())
				return
			}
			continue
		}
		failures = 0
		now := time.Now()
		c.mu.Lock()
		prevPos, prevState := c.Position, c.State
		quiet := now.Before(c.quietUntil)
		if st.State == cast.StatePlaying || st.State == cast.StatePaused {
			c.seenPlaying, c.everPlayed = true, true
			c.quietUntil = time.Time{}
			quiet = false
			c.Position = c.offset + st.Position
			if c.Mode == "direct" {
				c.Position = st.Position
			}
		}
		if st.State == cast.StatePlaying && prevState == cast.StatePlaying {
			if d := c.Position - prevPos; d > 0 && d < 3*castPoll.Seconds() {
				c.played += d
			}
		}
		if st.Volume >= 0 {
			c.Volume = st.Volume
		}
		var end, finished bool
		var why string
		switch {
		case st.State == cast.StateEnded:
			end, finished = true, true
		case st.State == cast.StateIdle && !quiet && c.seenPlaying:
			// Stopped on the TV, or it played to the end (DLNA can't tell).
			end = true
			finished = c.Duration > 0 && c.Position >= c.Duration-math.Min(120, c.Duration*0.05)
		case !c.seenPlaying && !quiet && st.State == cast.StateIdle,
			!c.seenPlaying && now.Sub(c.loadedAt) > castStartLimit:
			end, why = true, c.Device.Name+" couldn't play this video"
		default:
			c.State = st.State
		}
		tid := c.transcodeID
		report := now.Sub(c.reported) >= castReport || c.State != prevState
		everPlayed := c.everPlayed
		c.mu.Unlock()
		if end {
			if finished {
				c.mu.Lock()
				c.Position = c.Duration
				c.mu.Unlock()
			}
			s.endCast(c, why)
			return
		}
		if tid != "" {
			if sess := s.Transcoder.Get(tid); sess != nil {
				sess.Touch() // a paused TV stops fetching; the conversion waits for it
			}
		}
		if report && everPlayed {
			s.reportCast(c, false)
		}
	}
}

func (s *Server) reportCast(c *castSession, stopped bool) {
	c.mu.Lock()
	c.reported = time.Now()
	req := progressReq{
		PlaySessionID: c.playID, ItemID: c.ItemID, FileID: c.FileID, Position: c.Position, Duration: c.Duration,
		Paused: c.State == cast.StatePaused, Stopped: stopped, Mode: c.Mode, TranscodeID: c.transcodeID,
		Client: "Cast to " + c.Device.Name, Played: c.played, Lights: new(bool),
	}
	if stopped {
		req.TranscodeID = ""
	}
	uid, host := c.userID, c.Device.Host
	c.mu.Unlock()
	u, err := s.Store.UserByID(uid)
	if err != nil {
		return
	}
	if _, err := s.saveProgress(u, req, host); err != nil && !stopped {
		log.Printf("cast %s: saving progress: %v", c.ID, err)
	}
}

// endCast stops c on the TV (unless it already stopped there) and forgets
// it. why is shown to the remote when the cast failed.
func (s *Server) endCast(c *castSession, why string) {
	c.mu.Lock()
	if c.ended {
		c.mu.Unlock()
		return
	}
	c.ended = true
	if why != "" {
		c.Error = why
	}
	c.State = cast.StateIdle
	cancel, player, tid, seen := c.cancel, c.player, c.transcodeID, c.everPlayed
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if seen {
		s.reportCast(c, true)
	}
	if player != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			player.Stop(ctx)
			player.Close()
		}()
	}
	if tid != "" && s.Transcoder != nil {
		s.Transcoder.Stop(tid)
	}
	m := &s.casts
	m.mu.Lock()
	delete(m.sessions, c.ID)
	delete(m.tokens, c.token)
	m.mu.Unlock()
	if why != "" {
		log.Printf("cast %s: %s", c.ID, why)
	}
}

func (s *Server) castSessionsWhere(keep func(*castSession) bool) []*castSession {
	m := &s.casts
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*castSession
	for _, c := range m.sessions {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// --- the remote ---------------------------------------------------------------

func (s *Server) castSessionList(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	out := []castView{}
	for _, c := range s.castSessionsWhere(func(c *castSession) bool { return c.userID == u.ID }) {
		out = append(out, c.snapshot())
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) castSessionFor(w http.ResponseWriter, r *http.Request) *castSession {
	u := currentUser(r)
	m := &s.casts
	m.mu.Lock()
	c := m.sessions[chi.URLParam(r, "id")]
	m.mu.Unlock()
	if c == nil || (c.userID != u.ID && !u.IsAdmin) {
		writeErr(w, http.StatusNotFound, "that cast has ended")
		return nil
	}
	return c
}

func (s *Server) castSessionGet(w http.ResponseWriter, r *http.Request) {
	if c := s.castSessionFor(w, r); c != nil {
		writeJSON(w, http.StatusOK, c.snapshot())
	}
}

func (s *Server) castSessionStop(w http.ResponseWriter, r *http.Request) {
	if c := s.castSessionFor(w, r); c != nil {
		s.endCast(c, "")
		w.WriteHeader(http.StatusNoContent)
	}
}

type castControlReq struct {
	Action   string  `json:"action"` // play | pause | seek | volume
	Position float64 `json:"position"`
	Level    float64 `json:"level"`
	// AudioStream switches the audio track (a reload from where it is).
	AudioStream *int `json:"audioStream"`
}

func (s *Server) castSessionControl(w http.ResponseWriter, r *http.Request) {
	c := s.castSessionFor(w, r)
	if c == nil {
		return
	}
	var req castControlReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c.mu.Lock()
	mode, duration, pos := c.Mode, c.Duration, c.Position
	c.mu.Unlock()
	var err error
	switch req.Action {
	case "play":
		err = c.player.Play(ctx)
	case "pause":
		err = c.player.Pause(ctx)
	case "volume":
		if req.Level < 0 || req.Level > 1 {
			writeErr(w, http.StatusBadRequest, "volume goes from 0 to 1")
			return
		}
		err = c.player.SetVolume(ctx, req.Level)
		if errors.Is(err, cast.ErrUnsupported) {
			writeErr(w, http.StatusBadRequest, "use the TV's remote for the volume")
			return
		}
	case "seek", "audio":
		at := req.Position
		if req.Action == "audio" {
			at = pos
			if req.AudioStream == nil {
				writeErr(w, http.StatusBadRequest, "pick an audio track")
				return
			}
			c.mu.Lock()
			c.audio = *req.AudioStream
			c.mu.Unlock()
		}
		if at < 0 {
			at = 0
		}
		if duration > 0 && at > duration-1 {
			at = duration - 1
		}
		if mode == "direct" && req.Action == "seek" {
			err = c.player.Seek(ctx, at)
			if err == nil {
				c.mu.Lock()
				c.Position = at
				c.mu.Unlock()
			}
			break
		}
		// A conversion only covers what's been converted from where it
		// started; seeking (or another audio track) starts a new one there.
		var mf *store.MediaFile
		if mf, err = s.Store.File(c.FileID); err == nil {
			err = s.castLoad(ctx, c, mf, at)
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if req.Action == "play" || req.Action == "pause" {
		c.mu.Lock()
		if c.seenPlaying {
			c.State = map[string]string{"play": cast.StatePlaying, "pause": cast.StatePaused}[req.Action]
		}
		c.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, c.snapshot())
}
