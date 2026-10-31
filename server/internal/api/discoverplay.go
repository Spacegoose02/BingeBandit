package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/livetv"
)

// The TV app plays Discover videos in its own player rather than sending
// people off to the YouTube app. yt-dlp finds the video's stream, and the
// server passes it on: YouTube ties the stream to the address that asked
// for it, which the TV may not share (IPv6, or away from home).

// Tests replace these.
var (
	resolveYouTubeVideo = livetv.ResolveYouTubeVideo
	youtubePlaylist     = livetv.YouTubeGet
	openYouTube         = livetv.OpenYouTube
)

var reVideoKey = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// discoverStreamTTL is how long a resolved stream is reused. YouTube's
// addresses last some hours; a trailer is over long before this.
const discoverStreamTTL = time.Hour

type discoverStream struct {
	video    livetv.YouTubeVideo
	playlist []byte   // HLS: the media playlist, its URIs numbered s/0, s/1…
	segs     []string // HLS: the YouTube addresses behind those numbers
	at       time.Time
}

type discoverStreams struct {
	mu       sync.Mutex
	m        map[string]*discoverStream
	inflight map[string]*discoverResolve
}

// discoverResolve is a lookup under way. Everyone who asks for the same
// video while it runs waits on the one lookup.
type discoverResolve struct {
	done chan struct{}
	st   *discoverStream
	err  error
	code int // the HTTP status for err
}

// discoverResolveTimeout bounds one lookup. yt-dlp alone may take 45s (it
// solves YouTube's JavaScript challenge first), then the playlist is read.
const discoverResolveTimeout = 90 * time.Second

// discoverPrepareMax caps lookups started ahead of a press (a highlighted
// tile), so scrolling along a row doesn't start a yt-dlp for every video.
const discoverPrepareMax = 2

func (d *discoverStreams) get(id string) *discoverStream {
	d.mu.Lock()
	defer d.mu.Unlock()
	if st := d.m[id]; st != nil && time.Since(st.at) < discoverStreamTTL {
		return st
	}
	return nil
}

func (d *discoverStreams) put(id string, st *discoverStream) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.putLocked(id, st)
}

func (d *discoverStreams) putLocked(id string, st *discoverStream) {
	if d.m == nil {
		d.m = map[string]*discoverStream{}
	}
	for k, old := range d.m {
		if time.Since(old.at) >= discoverStreamTTL {
			delete(d.m, k)
		}
	}
	d.m[id] = st
}

func (d *discoverStreams) drop(id string) {
	d.mu.Lock()
	delete(d.m, id)
	d.mu.Unlock()
}

// resolve returns the lookup for a video: one already under way, or a new
// one. The lookup runs on its own, apart from the request that started it:
// a TV that stops waiting (or a person pressing again) doesn't throw the
// work away, and the next press finds the stream ready. With ahead set
// (a highlighted tile, not a press), nothing new starts once
// discoverPrepareMax lookups are running; it returns nil then.
func (d *discoverStreams) resolve(id string, ahead bool) *discoverResolve {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c := d.inflight[id]; c != nil {
		return c
	}
	if ahead && len(d.inflight) >= discoverPrepareMax {
		return nil
	}
	if d.inflight == nil {
		d.inflight = map[string]*discoverResolve{}
	}
	c := &discoverResolve{done: make(chan struct{})}
	d.inflight[id] = c
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), discoverResolveTimeout)
		defer cancel()
		c.st, c.code, c.err = lookUpDiscoverStream(ctx, id)
		d.mu.Lock()
		if c.err == nil {
			d.putLocked(id, c.st)
		}
		delete(d.inflight, id)
		d.mu.Unlock()
		close(c.done)
	}()
	return c
}

// lookUpDiscoverStream asks yt-dlp for a video's stream and, for HLS, reads
// its playlist.
func lookUpDiscoverStream(ctx context.Context, id string) (*discoverStream, int, error) {
	v, err := resolveYouTubeVideo(ctx, id)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, http.StatusGatewayTimeout, errors.New("Finding this video took too long. Try again in a moment.")
		}
		return nil, http.StatusBadGateway, errors.New("Couldn't play this video here: " + err.Error())
	}
	st := &discoverStream{video: v, at: time.Now()}
	if v.HLS {
		body, err := youtubePlaylist(ctx, v.URL)
		if err != nil {
			return nil, http.StatusBadGateway, errors.New("Couldn't read the video's playlist: " + err.Error())
		}
		st.playlist, st.segs = numberPlaylist(v.URL, body)
	}
	return st, 0, nil
}

// discoverVideoID reads the video id and checks it's on the Discover page,
// so the server only passes on videos an admin chose.
func (s *Server) discoverVideoID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "vid")
	if !reVideoKey.MatchString(id) {
		writeErr(w, http.StatusBadRequest, "bad video id")
		return "", false
	}
	ok, err := s.Store.HasDiscoverVideo(id)
	if err != nil {
		writeStoreErr(w, err)
		return "", false
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "that video isn't on Discover")
		return "", false
	}
	return id, true
}

// discoverStream resolves a video (or reuses one resolved lately) and, for
// HLS, reads its playlist. If the caller goes away first, the lookup carries
// on and is kept for next time.
func (s *Server) discoverStream(w http.ResponseWriter, r *http.Request, id string) (*discoverStream, bool) {
	if st := s.discoverStreams.get(id); st != nil {
		return st, true
	}
	c := s.discoverStreams.resolve(id, false)
	select {
	case <-c.done:
	case <-r.Context().Done():
		return nil, false
	}
	if c.err != nil {
		writeErr(w, c.code, c.err.Error())
		return nil, false
	}
	return c.st, true
}

// discoverPrepare starts finding a video's stream before it's pressed (the
// TV app asks when a tile is highlighted), so it starts at once when it is.
func (s *Server) discoverPrepare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.discoverVideoID(w, r)
	if !ok {
		return
	}
	if s.discoverStreams.get(id) == nil {
		s.discoverStreams.resolve(id, true)
	}
	w.WriteHeader(http.StatusAccepted)
}

// numberPlaylist swaps each URI in a media playlist for s/<n>, relative to
// the playlist, and returns the YouTube addresses in that order.
func numberPlaylist(base, body string) ([]byte, []string) {
	b, _ := url.Parse(base)
	var segs []string
	number := func(uri string) string {
		if b != nil {
			if u, err := b.Parse(uri); err == nil {
				uri = u.String()
			}
		}
		segs = append(segs, uri)
		return "s/" + strconv.Itoa(len(segs)-1)
	}
	var out strings.Builder
	for i, line := range strings.Split(body, "\n") {
		if i > 0 {
			out.WriteByte('\n')
		}
		line = strings.TrimRight(line, "\r")
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			if at := strings.Index(line, `URI="`); at >= 0 {
				start := at + len(`URI="`)
				if end := strings.IndexByte(line[start:], '"'); end >= 0 {
					line = line[:start] + number(line[start:start+end]) + line[start+end:]
				}
			}
		default:
			line = number(line)
		}
		out.WriteString(line)
	}
	return []byte(out.String()), segs
}

// discoverVideo resolves a Discover video for the TV app and says where to
// play it from: an HLS playlist or an MP4 file, both through the server.
func (s *Server) discoverVideo(w http.ResponseWriter, r *http.Request) {
	id, ok := s.discoverVideoID(w, r)
	if !ok {
		return
	}
	st, ok := s.discoverStream(w, r, id)
	if !ok {
		return
	}
	base := "/api/discover/videos/" + id
	if st.video.HLS {
		writeJSON(w, http.StatusOK, map[string]string{"url": base + "/index.m3u8", "type": "m3u8"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": base + "/video.mp4", "type": "mp4"})
}

func (s *Server) discoverVideoPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := s.discoverVideoID(w, r)
	if !ok {
		return
	}
	st, ok := s.discoverStream(w, r, id)
	if !ok {
		return
	}
	if !st.video.HLS {
		writeErr(w, http.StatusNotFound, "this video is an MP4 file")
		return
	}
	b := st.playlist
	if q := playlistQuery(r); q != "" {
		b = rewritePlaylist(b, q, false)
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) discoverVideoSegment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.discoverVideoID(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(chi.URLParam(r, "n"))
	st := s.discoverStreams.get(id)
	if err != nil || st == nil || n < 0 || n >= len(st.segs) {
		writeErr(w, http.StatusNotFound, "no such part")
		return
	}
	s.passOnYouTube(w, r, id, st.segs[n])
}

func (s *Server) discoverVideoFile(w http.ResponseWriter, r *http.Request) {
	id, ok := s.discoverVideoID(w, r)
	if !ok {
		return
	}
	st, ok := s.discoverStream(w, r, id)
	if !ok {
		return
	}
	if st.video.HLS {
		writeErr(w, http.StatusNotFound, "this video is an HLS stream")
		return
	}
	s.passOnYouTube(w, r, id, st.video.URL)
}

// passOnYouTube streams a file or segment from YouTube to the player, with
// its Range, so it can seek. When YouTube refuses (the address expired),
// the video is resolved afresh next time.
func (s *Server) passOnYouTube(w http.ResponseWriter, r *http.Request, id, link string) {
	resp, err := openYouTube(r.Context(), link, r.Header.Get("Range"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "YouTube: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusGone {
			s.discoverStreams.drop(id)
		}
		writeErr(w, http.StatusBadGateway, "YouTube answered "+resp.Status)
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
