package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// liveChannel is what non-admins see: no stream URLs (they often embed
// provider credentials), plus what's on now and next.
type liveChannel struct {
	ID       int64               `json:"id"`
	Number   float64             `json:"number"`
	Name     string              `json:"name"`
	Logo     string              `json:"logo,omitempty"`
	Group    string              `json:"group,omitempty"`
	EPGID    string              `json:"epgId,omitempty"`
	Favorite bool                `json:"favorite"`
	Now      *store.GuideProgram `json:"now,omitempty"`
	Next     *store.GuideProgram `json:"next,omitempty"`
}

func (s *Server) liveChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.Channels(currentUser(r).ID, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.withNowNext(chans))
}

func (s *Server) withNowNext(chans []*store.Channel) []liveChannel {
	now := time.Now().Unix()
	var ids []string
	for _, c := range chans {
		if c.EPGID != "" {
			ids = append(ids, c.EPGID)
		}
	}
	progs, _ := s.Store.Programs(ids, now, now+12*3600)
	byID := map[string][]store.GuideProgram{}
	for _, p := range progs {
		byID[p.EPGID] = append(byID[p.EPGID], p)
	}
	out := make([]liveChannel, 0, len(chans))
	for _, c := range chans {
		lc := liveChannel{ID: c.ID, Number: c.Number, Name: c.Name, Logo: logoURL(c.ID, c.Logo), Group: c.Group, EPGID: c.EPGID, Favorite: c.Favorite}
		list := byID[c.EPGID]
		for i := range list {
			if list[i].Start <= now && list[i].Stop > now {
				lc.Now = &list[i]
				if i+1 < len(list) {
					lc.Next = &list[i+1]
				}
				break
			}
			if list[i].Start > now && lc.Next == nil {
				lc.Next = &list[i]
			}
		}
		out = append(out, lc)
	}
	return out
}

// logoURL points clients at the server's logo cache (or the logo library)
// instead of the provider. The version changes with the logo, so a new logo
// shows up at once despite caching.
func logoURL(channelID int64, logo string) string {
	if logo == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(logo))
	return fmt.Sprintf("/api/live/channels/%d/logo?v=%s", channelID, hex.EncodeToString(sum[:4]))
}

// channelLogo serves a channel's logo from the cache. Only URLs already on a
// channel are fetched, so this can't be used to make arbitrary requests.
func (s *Server) channelLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	ch, err := s.Store.Channel(currentUser(r).ID, id)
	if err != nil || ch.Logo == "" {
		http.NotFound(w, r)
		return
	}
	if file, ok := strings.CutPrefix(ch.Logo, store.LibraryLogoPrefix); ok {
		s.serveLibraryLogo(w, r, file)
		return
	}
	path, ctype, err := s.Logos.Get(r.Context(), ch.Logo)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// SVGs from providers are served as images only; block scripts if opened directly.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	http.ServeFile(w, r, path)
}

// liveGuide returns channels and their programs for [from, to) (unix seconds).
func (s *Server) liveGuide(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	if from == 0 {
		from = time.Now().Add(-30 * time.Minute).Unix()
	}
	if to <= from || to-from > 24*3600 {
		to = from + 6*3600
	}
	chans, err := s.Store.Channels(currentUser(r).ID, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var ids []string
	for _, c := range chans {
		if c.EPGID != "" {
			ids = append(ids, c.EPGID)
		}
	}
	progs, err := s.Store.Programs(ids, from, to)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	byID := map[string][]store.GuideProgram{}
	for _, p := range progs {
		byID[p.EPGID] = append(byID[p.EPGID], p)
	}
	recs, _ := s.Store.RecordingStatuses(from, to)
	cats, err := s.Store.ChannelCategories(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"from": from, "to": to, "channels": s.withNowNext(chans), "programs": byID,
		"recordings": recs, "canRecord": s.canRecord(currentUser(r)), "categories": cats,
	})
}

func (s *Server) setChannelFavorite(fav bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r, "id")
		if !ok {
			writeErr(w, http.StatusBadRequest, "bad id")
			return
		}
		if err := s.Store.SetChannelFavorite(currentUser(r).ID, id, fav); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) watchableChannel(w http.ResponseWriter, r *http.Request) *store.Channel {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	ch, err := s.Store.Channel(currentUser(r).ID, id)
	if err == nil && !ch.Enabled {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return ch
}

// watchChannel joins the channel's shared relay as a web (HLS) viewer.
func (s *Server) watchChannel(w http.ResponseWriter, r *http.Request) {
	ch := s.watchableChannel(w, r)
	if ch == nil {
		return
	}
	v, err := s.Live.Join(ch, currentUser(r), clientIP(r), "hls")
	if err != nil {
		log.Printf("live tv: %s couldn't start channel %v %q: %v", currentUser(r).Username, ch.Number, ch.Name, err)
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	// Answer once the first segments exist. Players (ExoPlayer especially)
	// give up on a playlist request that hangs while the upstream connects
	// and ffmpeg starts, which showed up as timeouts when flipping channels.
	if _, err := s.Live.HLSFile(r.Context(), ch.ID, "live.m3u8"); err != nil {
		s.Live.Leave(v.ID)
		if r.Context().Err() == nil {
			log.Printf("live tv: %s couldn't start channel %v %q: %v", currentUser(r).Username, ch.Number, ch.Name, err)
			writeErr(w, http.StatusServiceUnavailable, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bufferSecs": s.Live.ChannelBufferSeconds(ch.ID),
		"stallSecs":  s.Live.StallSeconds(),
		"viewerId":   v.ID,
		"playlist":   fmt.Sprintf("/api/live/channels/%d/hls/live.m3u8?viewer=%s", ch.ID, v.ID),
	})
}

func (s *Server) liveHLS(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	name := chi.URLParam(r, "name")
	if vid := r.URL.Query().Get("viewer"); vid != "" && !s.Live.Touch(id, vid) {
		writeErr(w, http.StatusGone, "this viewing session ended; start the channel again")
		return
	}
	path, err := s.Live.HLSFile(r.Context(), id, name)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	f, err := stream.OpenShared(path)
	if err != nil {
		http.NotFound(w, r) // the segment rotated out between listing and fetching
		return
	}
	defer f.Close()
	if strings.HasSuffix(name, ".m3u8") {
		// Players poll the live playlist every few seconds; that poll (with
		// ?viewer=) is what keeps the viewer counted as watching.
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		b, err := io.ReadAll(f)
		if err != nil {
			writeErr(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		w.Write(s.Live.Playlist(id, b))
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeContent(w, r, name, time.Time{}, f)
}

// pingViewer keeps a web or app viewer counted while its player is paused
// and not fetching the playlist.
func (s *Server) pingViewer(w http.ResponseWriter, r *http.Request) {
	if !s.Live.Ping(chi.URLParam(r, "viewer")) {
		writeErr(w, http.StatusGone, "this viewing session ended; start the channel again")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// viewerBuffering is a player reporting that it has been buffering for a
// while; the relay may move the channel to its next stream.
func (s *Server) viewerBuffering(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seconds float64 `json:"seconds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	switched, reason := s.Live.ReportBuffering(chi.URLParam(r, "viewer"), req.Seconds)
	writeJSON(w, http.StatusOK, map[string]any{"switched": switched, "reason": reason})
}

// testEncoder tries the hardware encoder (the saved one, or ?encoder=) the
// way live TV uses it and reports ffmpeg's error if it fails.
func (s *Server) testEncoder(w http.ResponseWriter, r *http.Request) {
	enc := r.URL.Query().Get("encoder")
	if enc == "" {
		enc = s.Store.Setting(store.SettingHWEncoder)
	}
	if !validEncoders[enc] {
		writeErr(w, http.StatusBadRequest, "unknown hardware encoder")
		return
	}
	if err := s.Live.TestEncoder(r.Context(), enc); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) leaveChannel(w http.ResponseWriter, r *http.Request) {
	s.Live.Leave(chi.URLParam(r, "viewer"))
	w.WriteHeader(http.StatusNoContent)
}

// liveTS streams a channel as raw MPEG-TS for external players (VLC, Kodi,
// TiviMate…). It shares the same upstream connection as web viewers.
func (s *Server) liveTS(w http.ResponseWriter, r *http.Request) {
	ch := s.watchableChannel(w, r)
	if ch == nil {
		return
	}
	v, err := s.Live.Join(ch, currentUser(r), clientIP(r), "ts")
	if err != nil {
		log.Printf("live tv: %s (%s, %s) couldn't open channel %v %q: %v", currentUser(r).Username, clientIP(r), r.UserAgent(), ch.Number, ch.Name, err)
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	// External players (Plex's tuner, IPTV apps) leave no other trace, so
	// say when one opens and closes a channel.
	log.Printf("live tv: %s (%s, %s) opened channel %v %q", currentUser(r).Username, clientIP(r), r.UserAgent(), ch.Number, ch.Name)
	began := time.Now()
	defer func() {
		log.Printf("live tv: %s (%s) closed channel %v %q after %s", currentUser(r).Username, clientIP(r), ch.Number, ch.Name, time.Since(began).Round(time.Second))
	}()
	defer s.Live.Leave(v.ID)
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case <-v.Done():
			return
		case chunk := <-v.Chunks():
			if _, err := w.Write(chunk); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// requestToken recovers the caller's token so generated playlist URLs work
// in apps that can't send headers or cookies.
func requestToken(r *http.Request) string {
	if t, _, _ := strings.Cut(r.URL.Query().Get("token"), "?"); t != "" {
		return t
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// livePlaylist exports the channel lineup as M3U for other IPTV apps.
func (s *Server) livePlaylist(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.Channels(currentUser(r).ID, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	token, base := requestToken(r), baseURL(r)
	ids := xmltvIDs(chans)
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U x-tvg-url=\"%s/api/live/epg.xml?token=%s\"\n", base, token)
	clean := strings.NewReplacer(`"`, "'", "\n", " ", "\r", " ")
	for _, c := range chans {
		logo := c.Logo
		if logo != "" {
			logo = base + logoURL(c.ID, logo) + "&token=" + token
		}
		fmt.Fprintf(&b, "#EXTINF:-1 tvg-id=\"%s\" tvg-chno=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n%s/api/live/channels/%d/stream.ts?token=%s\n",
			clean.Replace(ids[c.ID]), strconv.FormatFloat(c.Number, 'f', -1, 64), clean.Replace(logo), clean.Replace(c.Group),
			clean.Replace(c.Name), base, c.ID, token)
	}
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="bingebandit.m3u"`)
	w.Write([]byte(b.String()))
}

// liveEPG exports the guide for the channel lineup as XMLTV.
func (s *Server) liveEPG(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.Channels(currentUser(r).ID, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	now := time.Now()
	var ids []string
	for _, c := range chans {
		if c.EPGID != "" {
			ids = append(ids, c.EPGID)
		}
	}
	progs, err := s.Store.Programs(ids, now.Add(-2*time.Hour).Unix(), now.Add(72*time.Hour).Unix())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type xmlText struct {
		Value string `xml:",chardata"`
	}
	type xmlIcon struct {
		Src string `xml:"src,attr"`
	}
	type xmlChan struct {
		ID    string    `xml:"id,attr"`
		Names []xmlText `xml:"display-name"`
		Icon  *xmlIcon  `xml:"icon,omitempty"`
	}
	type xmlProg struct {
		Start    string       `xml:"start,attr"`
		Stop     string       `xml:"stop,attr"`
		Channel  string       `xml:"channel,attr"`
		Title    xmlText      `xml:"title"`
		SubTitle *xmlText     `xml:"sub-title,omitempty"`
		Desc     *xmlText     `xml:"desc,omitempty"`
		Category *xmlText     `xml:"category,omitempty"`
		Icon     *xmlIcon     `xml:"icon,omitempty"`
		Episodes []xmlEpisode `xml:"episode-num"`
	}
	opt := func(s string) *xmlText {
		if s == "" {
			return nil
		}
		return &xmlText{s}
	}
	token, base := requestToken(r), baseURL(r)
	doc := struct {
		XMLName  xml.Name  `xml:"tv"`
		Gen      string    `xml:"generator-info-name,attr"`
		Channels []xmlChan `xml:"channel"`
		Programs []xmlProg `xml:"programme"`
	}{Gen: "BingeBandit"}
	// Every channel gets its own guide channel, and every hour of the guide
	// has something on: Plex can't play a channel, or a time, it has no
	// programme for ("item not known when attempting to build decision").
	xids := xmltvIDs(chans)
	byEPG := map[string][]store.GuideProgram{}
	for _, p := range progs {
		byEPG[p.EPGID] = append(byEPG[p.EPGID], p)
	}
	const layout = "20060102150405 -0700"
	from, to := now.Add(-2*time.Hour).Truncate(time.Hour).Unix(), now.Add(72*time.Hour).Unix()
	for _, c := range chans {
		// The number as a second name lets Plex match guide channels to the
		// tuner lineup on its own.
		xc := xmlChan{ID: xids[c.ID], Names: []xmlText{{c.Name}, {strconv.FormatFloat(c.Number, 'f', -1, 64)}}}
		if c.Logo != "" {
			// Through BingeBandit's logo cache rather than the provider's
			// address, which Plex may not be able to fetch.
			xc.Icon = &xmlIcon{base + logoURL(c.ID, c.Logo) + "&token=" + token}
		}
		doc.Channels = append(doc.Channels, xc)
		for _, p := range fillGuide(byEPG[c.EPGID], c.Name, from, to) {
			xp := xmlProg{
				Start: time.Unix(p.Start, 0).UTC().Format(layout), Stop: time.Unix(p.Stop, 0).UTC().Format(layout), Channel: xids[c.ID],
				Title: xmlText{p.Title}, SubTitle: opt(p.Subtitle), Desc: opt(p.Description), Category: opt(p.Category),
				Episodes: episodeNums(p),
			}
			if p.Icon != "" {
				xp.Icon = &xmlIcon{p.Icon}
			}
			doc.Programs = append(doc.Programs, xp)
		}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", " ")
	enc.Encode(doc)
}

// episodeNums tells Plex a programme is an episode of a show. Without an
// episode number Plex takes every programme for a movie (its guide showed
// "Friends" as a movie), so anything not marked as a film also gets its air
// time as an original-air-date, the number Plex uses for news, sports and
// other shows without seasons.
func episodeNums(p store.GuideProgram) []xmlEpisode {
	var out []xmlEpisode
	switch e := strings.TrimSpace(p.Episode); {
	case e == "":
	case strings.Count(e, ".") == 2:
		out = append(out, xmlEpisode{"xmltv_ns", e})
	default:
		out = append(out, xmlEpisode{"onscreen", e})
	}
	if c := strings.ToLower(p.Category); !strings.Contains(c, "movie") && !strings.Contains(c, "film") {
		out = append(out, xmlEpisode{"original-air-date", time.Unix(p.Start, 0).UTC().Format("2006-01-02 15:04:05")})
	}
	return out
}

type xmlEpisode struct {
	System string `xml:"system,attr"`
	Value  string `xml:",chardata"`
}

// xmltvIDs is each channel's id in the exported guide and playlist: its
// guide channel's id, or its own (bb.<id>) when it has none or an earlier
// channel already uses that guide channel.
func xmltvIDs(chans []*store.Channel) map[int64]string {
	ids := make(map[int64]string, len(chans))
	used := map[string]bool{}
	for _, c := range chans {
		id := c.EPGID
		if id == "" || used[id] {
			id = fmt.Sprintf("bb.%d", c.ID)
		}
		used[id] = true
		ids[c.ID] = id
	}
	return ids
}

// fillGuide returns a channel's programmes (sorted by start) with every gap
// between from and to filled by hour-long placeholders named after the
// channel.
func fillGuide(progs []store.GuideProgram, name string, from, to int64) []store.GuideProgram {
	out := make([]store.GuideProgram, 0, len(progs)+8)
	fill := func(start, stop int64) {
		for start < stop {
			end := min(stop, (start/3600+1)*3600)
			out = append(out, store.GuideProgram{Start: start, Stop: end, Title: name})
			start = end
		}
	}
	at := from
	for _, p := range progs {
		if p.Stop <= at {
			continue
		}
		if p.Start > at {
			fill(at, p.Start)
		}
		out = append(out, p)
		at = max(at, p.Stop)
	}
	fill(at, to)
	return out
}

// ---- admin ------------------------------------------------------------------

func (s *Server) liveSources(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.Store.LiveSources("")
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, srcs)
}

func (s *Server) saveLiveSource(w http.ResponseWriter, r *http.Request) {
	var src store.LiveSource
	if err := readJSON(r, &src); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if id, ok := idParam(r, "id"); ok {
		existing, err := s.Store.LiveSource(id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		src.ID, src.Kind = id, existing.Kind
	} else if src.Kind != "m3u" && src.Kind != "epg" {
		writeErr(w, http.StatusBadRequest, "kind must be m3u or epg")
		return
	}
	src.Name, src.URL = strings.TrimSpace(src.Name), strings.TrimSpace(src.URL)
	if src.Name == "" || src.URL == "" {
		writeErr(w, http.StatusBadRequest, "name and URL are required")
		return
	}
	if src.RefreshHours <= 0 {
		src.RefreshHours = 24
	}
	isNew := src.ID == 0
	if err := s.Store.SaveLiveSource(&src); err != nil {
		writeStoreErr(w, err)
		return
	}
	if isNew {
		go livetv.Refresh(s.ctx, s.Store, &src)
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) deleteLiveSource(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteLiveSource(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshLiveSource re-imports a source now and reports the outcome.
func (s *Server) refreshLiveSource(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	src, err := s.Store.LiveSource(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := livetv.Refresh(r.Context(), s.Store, src); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	src, _ = s.Store.LiveSource(id)
	writeJSON(w, http.StatusOK, src)
}

type sourceGroups struct {
	NewGroupsEnabled bool                `json:"newGroupsEnabled"`
	Groups           []store.SourceGroup `json:"groups"`
}

func (s *Server) liveSourceGroups(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	src, err := s.Store.LiveSource(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	groups, err := s.Store.SourceGroups(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sourceGroups{NewGroupsEnabled: src.NewGroupsEnabled, Groups: groups})
}

// saveLiveSourceGroups stores the group filter; the caller refreshes the
// source to apply it.
func (s *Server) saveLiveSourceGroups(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req sourceGroups
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	src, err := s.Store.LiveSource(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if src.Kind != "m3u" {
		writeErr(w, http.StatusBadRequest, "only playlists have groups")
		return
	}
	enabled := map[string]bool{}
	for _, g := range req.Groups {
		enabled[g.Name] = g.Enabled
	}
	if err := s.Store.SaveSourceGroups(id, req.NewGroupsEnabled, enabled); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) liveStreams(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := pageParams(r)
	source, _ := strconv.ParseInt(q.Get("source"), 10, 64)
	streams, total, err := s.Store.LiveStreams(store.StreamQuery{SourceID: source, Group: q.Get("group"), Search: strings.TrimSpace(q.Get("q")), Offset: offset, Limit: limit})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": streams, "total": total})
}

func (s *Server) liveGroups(w http.ResponseWriter, r *http.Request) {
	source, _ := strconv.ParseInt(r.URL.Query().Get("source"), 10, 64)
	groups, err := s.Store.StreamGroups(source)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

// adminChannel adds where clients load the logo from; Logo stays the saved
// value (a provider URL or a library reference) for editing.
type adminChannel struct {
	*store.Channel
	LogoURL string `json:"logoUrl,omitempty"`
}

func (s *Server) adminChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.Channels(currentUser(r).ID, true)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]adminChannel, 0, len(chans))
	for _, c := range chans {
		out = append(out, adminChannel{c, logoURL(c.ID, c.Logo)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createChannels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StreamIDs   []int64 `json:"streamIds"`
		StartNumber float64 `json:"startNumber"`
	}
	if err := readJSON(r, &req); err != nil || len(req.StreamIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "pick at least one stream")
		return
	}
	if req.StartNumber <= 0 {
		req.StartNumber = 1
	}
	n, err := s.Store.CreateChannelsFromStreams(req.StreamIDs, req.StartNumber)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"created": n})
}

func (s *Server) updateChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		store.Channel
		StreamIDs []int64 `json:"streamIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.ID = id
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || req.Number <= 0 {
		writeErr(w, http.StatusBadRequest, "a channel needs a name and a number above 0")
		return
	}
	if msg := s.checkLogo(req.Logo); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.Store.UpdateChannel(&req.Channel, req.StreamIDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.Live.Stop(id) // viewers reconnect with the new stream list
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	s.Live.Stop(id)
	if err := s.Store.DeleteChannel(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) epgChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.EPGChannels(strings.TrimSpace(r.URL.Query().Get("q")), 50)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// What's on now helps tell look-alike guide channels apart.
	type withNow struct {
		store.EPGChannel
		Now string `json:"now,omitempty"`
	}
	ids := make([]string, len(list))
	for i, c := range list {
		ids[i] = c.ID
	}
	now := time.Now().Unix()
	progs, err := s.Store.Programs(ids, now, now+1)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	titles := map[string]string{}
	for _, p := range progs {
		titles[p.EPGID] = p.Title
	}
	out := make([]withNow, len(list))
	for i, c := range list {
		out[i] = withNow{c, titles[c.ID]}
	}
	writeJSON(w, http.StatusOK, out)
}

// epgPreview lists a guide channel's programmes from now through the next few
// hours, so an admin can check a match before saving it.
func (s *Server) epgPreview(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "missing id")
		return
	}
	now := time.Now().Unix()
	progs, err := s.Store.Programs([]string{id}, now, now+6*3600)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if len(progs) > 12 {
		progs = progs[:12]
	}
	writeJSON(w, http.StatusOK, progs)
}

// liveActivity reports every running relay with its viewers and what's airing.
func (s *Server) liveActivity(w http.ResponseWriter, r *http.Request) {
	stats := s.Live.Stats()
	type withNow struct {
		livetv.RelayStat
		Now *store.GuideProgram `json:"now,omitempty"`
	}
	now := time.Now().Unix()
	out := make([]withNow, 0, len(stats))
	for _, st := range stats {
		st.Logo = logoURL(st.ChannelID, st.Logo)
		wn := withNow{RelayStat: st}
		if st.EPGID != "" {
			if progs, err := s.Store.Programs([]string{st.EPGID}, now, now+1); err == nil && len(progs) > 0 {
				wn.Now = &progs[0]
			}
		}
		out = append(out, wn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) stopRelay(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	s.Live.Stop(id)
	log.Printf("live tv: %s stopped channel %d", currentUser(r).Username, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) kickViewer(w http.ResponseWriter, r *http.Request) {
	s.Live.Leave(chi.URLParam(r, "viewer"))
	w.WriteHeader(http.StatusNoContent)
}
