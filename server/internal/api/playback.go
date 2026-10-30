package api

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

var videoMime = map[string]string{
	".mkv": "video/x-matroska", ".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm",
	".mov": "video/quicktime", ".avi": "video/x-msvideo", ".ts": "video/mp2t", ".m2ts": "video/mp2t",
}

// playableFile loads the {id} media file if the current user can see its
// library, writing the error response otherwise.
func (s *Server) playableFile(w http.ResponseWriter, r *http.Request) *store.MediaFile {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	mf, err := s.Store.File(id)
	if err == nil {
		_, err = s.Store.Item(currentUser(r).ID, mf.ItemID)
	}
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return mf
}

// canDownload reports whether u may save original files: admins always can,
// everyone else only when the server allows downloads.
func (s *Server) canDownload(u *store.User) bool {
	return u.IsAdmin || s.Store.Setting(store.SettingAllowDownloads) == "true"
}

func (s *Server) directPlay(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	download := r.URL.Query().Get("download") == "1"
	if download && !s.canDownload(currentUser(r)) {
		writeErr(w, http.StatusForbidden, "downloads are disabled on this server")
		return
	}
	f, err := os.Open(mf.Path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "file is not available")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot read file")
		return
	}
	ext := strings.ToLower(filepath.Ext(mf.Path))
	if ct, ok := videoMime[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}
	if download {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(mf.Path)}))
	}
	http.ServeContent(w, r, filepath.Base(mf.Path), st.ModTime(), f)
}

var textSubtitleCodecs = map[string]bool{"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true, "mov_text": true, "text": true}

// subtitle extracts an embedded text subtitle stream as WebVTT and caches it.
// Cue times are file time, which is what the player tracks even mid-transcode.
func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(strings.TrimSuffix(chi.URLParam(r, "stream"), ".vtt"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad stream")
		return
	}
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	var sub *store.Stream
	for i, st := range mf.Streams {
		if st.Index == index && st.Type == "subtitle" && textSubtitleCodecs[st.Codec] {
			sub = &mf.Streams[i]
		}
	}
	if sub == nil {
		writeErr(w, http.StatusNotFound, "no text subtitle stream with that index")
		return
	}
	// Embedded streams come out of the video; sidecars are converted from
	// their own file. The size in the cache name invalidates stale copies.
	input, mapping := mf.Path, fmt.Sprintf("0:%d", index)
	name := fmt.Sprintf("%d-%d-%d.vtt", mf.ID, index, mf.Size)
	if sub.External != "" {
		input, mapping = filepath.Join(filepath.Dir(mf.Path), sub.External), "0:s:0"
		info, err := os.Stat(input)
		if err != nil {
			writeErr(w, http.StatusNotFound, "subtitle file is missing")
			return
		}
		// Sidecar indexes shift when files are added, so key by name too.
		h := fnv.New32a()
		h.Write([]byte(sub.External))
		name = fmt.Sprintf("%d-%d-%d-%08x.vtt", mf.ID, index, info.Size(), h.Sum32())
	}
	cache := filepath.Join(s.DataDir, "cache", "subtitles", name)
	if _, err := os.Stat(cache); err != nil {
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "cache folder not writable")
			return
		}
		tmp := cache + ".tmp.vtt"
		cmd := exec.CommandContext(r.Context(), s.FFmpeg, "-nostdin", "-loglevel", "error", "-y", "-i", input,
			"-map", mapping, "-f", "webvtt", tmp)
		media.HideWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			os.Remove(tmp)
			log.Printf("subtitle %d:%d: %v %s", mf.ID, index, err, out)
			writeErr(w, http.StatusInternalServerError, "could not extract subtitles")
			return
		}
		if err := os.Rename(tmp, cache); err != nil {
			writeErr(w, http.StatusInternalServerError, "could not cache subtitles")
			return
		}
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	http.ServeFile(w, r, cache)
}

func (s *Server) startTranscode(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	opts := stream.Options{AudioStream: -1, BurnSubtitle: -1}
	if err := readJSON(r, &opts); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if old := s.Transcoder.Get(opts.Replace); old == nil || old.UserID != currentUser(r).ID {
		opts.Replace = ""
	}
	sess, err := s.Transcoder.Start(mf, currentUser(r).ID, opts)
	if err != nil {
		log.Printf("transcode: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not start transcoder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session":  sess,
		"playlist": fmt.Sprintf("/api/transcode/%s/%s", sess.ID, sess.Playlist()),
	})
}

// transcodeAccessEvery is how often a running transcode re-checks that its
// user can still see the file, so taking a library away (or deleting the
// user or library) also ends playback that already started.
const transcodeAccessEvery = 30 * time.Second

func (s *Server) transcodeSession(w http.ResponseWriter, r *http.Request) *stream.Session {
	sess := s.Transcoder.Get(chi.URLParam(r, "sid"))
	if sess == nil || (sess.UserID != currentUser(r).ID && !currentUser(r).IsAdmin) ||
		(sess.AccessCheckDue(transcodeAccessEvery) && !s.transcodeAllowed(sess)) {
		writeErr(w, http.StatusNotFound, "transcode session not found")
		return nil
	}
	return sess
}

// transcodeAllowed reports whether the user who started sess can still see
// its file, and stops the session when they can't.
func (s *Server) transcodeAllowed(sess *stream.Session) bool {
	mf, err := s.Store.File(sess.FileID)
	if err == nil {
		_, err = s.Store.Item(sess.UserID, mf.ItemID)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return true // allowed, or a passing database error that shouldn't end playback
	}
	log.Printf("transcode %s: user %d can no longer see file %d; stopping", sess.ID, sess.UserID, sess.FileID)
	s.Transcoder.Stop(sess.ID)
	return false
}

// recheckTranscodes ends running transcodes whose users lost access. It runs
// right after library access changes.
func (s *Server) recheckTranscodes() {
	if s.Transcoder == nil {
		return
	}
	for _, sess := range s.Transcoder.List() {
		s.transcodeAllowed(sess)
	}
}

func (s *Server) transcodeFile(w http.ResponseWriter, r *http.Request) {
	sess := s.transcodeSession(w, r)
	if sess == nil {
		return
	}
	name := chi.URLParam(r, "name")
	path, err := sess.WaitFile(r.Context(), name)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("transcode %s: %v", sess.ID, err)
		writeErr(w, http.StatusGatewayTimeout, "transcoder is not producing output")
		return
	}
	f, err := stream.OpenShared(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if strings.HasSuffix(name, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		if q := playlistQuery(r); q != "" {
			b, err := io.ReadAll(f)
			if err != nil {
				writeErr(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			w.Write(rewritePlaylist(b, q, r.URL.Query().Get("fromStart") == "1"))
			return
		}
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeContent(w, r, name, time.Time{}, f)
}

func (s *Server) stopTranscode(w http.ResponseWriter, r *http.Request) {
	if sess := s.transcodeSession(w, r); sess != nil {
		s.Transcoder.Stop(sess.ID)
		w.WriteHeader(http.StatusNoContent)
	}
}

type progressReq struct {
	PlaySessionID string  `json:"playSessionId"`
	ItemID        int64   `json:"itemId"`
	FileID        int64   `json:"fileId"`
	Position      float64 `json:"position"`
	Duration      float64 `json:"duration"`
	Paused        bool    `json:"paused"`
	Stopped       bool    `json:"stopped"`
	Mode          string  `json:"mode"`
	TranscodeID   string  `json:"transcodeId"`
	Client        string  `json:"client"`
	Played        float64 `json:"played"` // seconds actually played this session
	// Lights is the device's Hue Sync switch; apps that predate it leave it out (on).
	Lights *bool `json:"lights"`
	// LightsArea is the Hue room or zone this device's lights are in ("" = all of them).
	LightsArea string `json:"lightsArea"`
	// LightsMode is the device's own Hue Sync type ("sync" or "theater"); "" uses the profile's.
	LightsMode string `json:"lightsMode"`
}

// progress saves the resume point and keeps the admin "Now Playing" list current.
func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	var req progressReq
	if err := readJSON(r, &req); err != nil || req.ItemID == 0 {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	it, err := s.Store.Item(u.ID, req.ItemID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	watched, err := s.Store.SaveProgress(u.ID, it.ID, req.Position, req.Duration)
	if err != nil {
		writeStoreErr(w, err)
		return
	}

	if watched && it.Kind == "movie" {
		s.Store.RemoveFromWatchlist(u.ID, it.ID) // finished: no longer "to watch"
	}
	if req.PlaySessionID != "" {
		prev, had := s.Activity.Get(req.PlaySessionID)
		np := s.Activity.Update(s.nowPlaying(u, it, req, clientIP(r)))
		s.emitPlayer(prev, had, np, it, req.Stopped)
		s.recordPlay(np, it, watched)
		if watched && !it.Watch.Watched {
			s.Emit("playback.finished", s.playbackEvent(np, it))
		}
		if watched && (req.Stopped || !it.Watch.Watched) {
			s.Trakt.PushNew(u.ID) // no-op unless the user turned on Trakt sync
		}
		if req.Stopped {
			s.Activity.Stop(req.PlaySessionID)
			// Only plays that got a playback.start (see minRecordedPlay) get a stop.
			if np.PlayID != 0 || np.Watched >= minRecordedPlay {
				s.Emit("playback.stop", s.playbackEvent(np, it))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"watched": watched})
}

// emitPlayer sends the live player events, which follow the player itself
// (unlike playback.*, which wait for 30 s of play): player.play on a
// session's first ping, player.pause and player.resume, player.stop, and
// player.progress on every ping (for plugins that follow the position).
func (s *Server) emitPlayer(prev stream.NowPlaying, had bool, np stream.NowPlaying, it *store.Item, stopped bool) {
	ev := s.playerEvent(np, it)
	switch {
	case !had || prev.ItemID != np.ItemID:
		s.Emit("player.play", ev)
	case prev.Paused != np.Paused && np.Paused:
		s.Emit("player.pause", ev)
	case prev.Paused != np.Paused:
		s.Emit("player.resume", ev)
	}
	if stopped {
		ev["reason"] = "stopped"
		s.Emit("player.stop", ev)
		return
	}
	s.Emit("player.progress", ev)
}

// playerExpired reports a player that stopped pinging (closed tab, lost network).
func (s *Server) playerExpired(np stream.NowPlaying) {
	it, err := s.Store.Item(0, np.ItemID)
	if err != nil {
		return
	}
	ev := s.playerEvent(np, it)
	ev["reason"] = "timeout"
	s.Emit("player.stop", ev)
}

func (s *Server) playerEvent(np stream.NowPlaying, it *store.Item) map[string]any {
	ev := s.playbackEvent(np, it)
	ev["sessionId"], ev["fileId"], ev["paused"], ev["lights"], ev["lightsArea"], ev["lightsMode"] = np.ID, np.FileID, np.Paused, np.Lights, np.LightsArea, np.LightsMode
	return ev
}

// playbackEvent is the webhook payload for playback events.
func (s *Server) playbackEvent(np stream.NowPlaying, it *store.Item) map[string]any {
	ev := map[string]any{
		"user": np.Username, "userId": np.UserID, "itemId": it.ID, "kind": it.Kind, "title": np.Title, "subtitle": np.Subtitle,
		"year": it.Year, "client": np.Client, "ip": np.IP, "local": np.Local, "mode": np.Mode,
		"position": np.Position, "duration": np.Duration, "watchedSeconds": int(np.Watched),
		"tmdbId": it.TMDBID,
	}
	if it.Kind == "episode" {
		ev["episodeTitle"] = it.Title
		if it.SeasonNumber != nil && it.EpisodeNumber != nil {
			ev["season"], ev["episode"] = *it.SeasonNumber, *it.EpisodeNumber
		}
		if show, err := s.Store.Item(0, it.ShowID); err == nil {
			ev["showTmdbId"], ev["year"] = show.TMDBID, show.Year
		}
	}
	return ev
}

// minRecordedPlay keeps accidental clicks out of the history.
const minRecordedPlay = 30

// recordPlay creates or updates the session's play-history row.
func (s *Server) recordPlay(np stream.NowPlaying, it *store.Item, completed bool) {
	if np.PlayID == 0 {
		if np.Watched < minRecordedPlay {
			return
		}
		p := &store.Play{
			UserID: np.UserID, ItemID: it.ID, Title: np.Title, Subtitle: np.Subtitle, Kind: it.Kind,
			LibraryID: it.LibraryID, ShowID: it.ShowID, StartedAt: np.StartedAt, StoppedAt: np.UpdatedAt,
			Watched: int64(np.Watched), Completed: completed, Mode: np.Mode, Client: np.Client, IP: np.IP,
		}
		id, err := s.Store.StartPlay(p)
		if err != nil {
			log.Printf("history: %v", err)
			return
		}
		s.Activity.SetPlayID(np.ID, id)
		s.Emit("playback.start", s.playbackEvent(np, it))
		return
	}
	if err := s.Store.UpdatePlay(np.PlayID, np.UpdatedAt, int64(np.Watched), completed, np.Mode); err != nil {
		log.Printf("history: %v", err)
	}
}

func (s *Server) nowPlaying(u *store.User, it *store.Item, req progressReq, ip string) stream.NowPlaying {
	np := stream.NowPlaying{
		ID: req.PlaySessionID, UserID: u.ID, Username: u.Username, Name: memberName(u), ItemID: it.ID, Title: it.Title, Poster: it.Poster,
		FileID: req.FileID, Mode: req.Mode, Position: req.Position, Duration: req.Duration, Paused: req.Paused,
		IP: ip, Local: s.isLocal(ip), Client: req.Client, TranscodeID: req.TranscodeID, Watched: req.Played,
		Lights: req.Lights == nil || *req.Lights, LightsArea: req.LightsArea, LightsMode: req.LightsMode,
	}
	if it.Kind == "episode" && it.SeasonNumber != nil && it.EpisodeNumber != nil {
		np.Title = it.ShowTitle
		np.Subtitle = fmt.Sprintf("S%d · E%d %s", *it.SeasonNumber, *it.EpisodeNumber, it.Title)
		np.Poster = it.ShowPoster
	}
	if f, err := s.Store.File(req.FileID); err == nil {
		np.Source = describeSource(f)
	}
	if req.Mode == "transcode" {
		if t := s.Transcoder.Get(req.TranscodeID); t != nil {
			audio := fmt.Sprintf("%s %s", strings.ToUpper(t.AudioCodec), channelLabel(t.AudioChannels))
			if t.AudioCopy {
				audio += " (copy)"
			}
			if t.VideoCopy {
				np.Target = "Video copy · " + audio
			} else {
				res := resolutionLabel(t.Height)
				if len(t.Variants) > 1 {
					labels := make([]string, len(t.Variants))
					for i, h := range t.Variants {
						labels[i] = resolutionLabel(h)
					}
					res = "Adaptive " + strings.Join(labels, "/")
				}
				np.Target = fmt.Sprintf("%s H264 (%s) · %s", res, t.Encoder, audio)
				if t.Tonemap {
					np.Target += " · tone mapped"
				}
				if t.BurnSubtitle >= 0 {
					np.Target += " · subtitles burned in"
				}
			}
		}
	} else {
		np.Target = "Direct play"
	}
	return np
}

func describeSource(f *store.MediaFile) string {
	parts := []string{}
	if f.VideoCodec != "" {
		v := resolutionLabel(f.Height) + " " + strings.ToUpper(f.VideoCodec)
		if f.HDR {
			v += " HDR"
		}
		parts = append(parts, v)
	}
	if f.AudioCodec != "" {
		parts = append(parts, fmt.Sprintf("%s %s", strings.ToUpper(f.AudioCodec), channelLabel(f.AudioChannels)))
	}
	return strings.Join(parts, " · ")
}

func resolutionLabel(h int) string {
	switch {
	case h >= 2000:
		return "4K"
	case h >= 1000:
		return "1080p"
	case h >= 700:
		return "720p"
	case h >= 560:
		return "576p"
	case h > 0:
		return "480p"
	}
	return "SD"
}

func channelLabel(n int) string {
	switch n {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	}
	return fmt.Sprintf("%dch", n)
}
