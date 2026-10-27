package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bingebandit/internal/store"
)

// settingNotifyWatermark is the added_at of the newest episode already
// considered for notifications. It is internal, so it is not in SettingDefaults.
const settingNotifyWatermark = "notify_episodes_since"

const notifyInterval = 5 * time.Minute

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	list, unread, err := s.Store.Notifications(currentUser(r).ID, 50)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": list, "unread": unread})
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad body")
			return
		}
	}
	if err := s.Store.ReadNotifications(currentUser(r).ID, body.IDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearNotifications(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.ClearNotifications(currentUser(r).ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// notifyEvent turns server events into notifications for the user they concern.
func (s *Server) notifyEvent(event string, data any) {
	r, ok := data.(*store.Recording)
	if !ok || (event != "recording.completed" && event != "recording.failed") || r.UserID == 0 {
		return
	}
	if p, err := s.Store.UserPrefs(r.UserID); err != nil || !p.NotifyRecordings {
		return
	}
	n := &store.Notification{UserID: r.UserID, Kind: "recording", Link: "/recordings"}
	if event == "recording.completed" {
		n.Title = "Recorded " + r.Title
		n.Body = r.ChannelName
		if r.Error != "" {
			n.Body += " · " + r.Error
		}
	} else {
		n.Title = "Recording failed: " + r.Title
		n.Body = firstNonEmptyStr(r.Error, r.ChannelName)
	}
	if err := s.Store.AddNotification(n); err != nil {
		slog.Warn("notification", "err", err)
	}
}

func firstNonEmptyStr(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// RunNotifications looks for newly added episodes every few minutes until
// ctx ends and tells the people who follow each show.
func (s *Server) RunNotifications(ctx context.Context) {
	t := time.NewTicker(time.Minute) // followed Live TV shows need to be on time
	defer t.Stop()
	var episodes time.Time
	for {
		if time.Since(episodes) >= notifyInterval {
			episodes = time.Now()
			s.notifyNewEpisodes()
			if err := s.Store.PruneNotifications(); err != nil {
				slog.Warn("prune notifications", "err", err)
			}
		}
		s.notifyLiveShows(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// notifyLiveShows tells people when a Live TV show they follow is about to
// start on a channel they can watch, once per airing.
func (s *Server) notifyLiveShows(now time.Time) {
	users, err := s.Store.ListUsers()
	if err != nil {
		return
	}
	for _, u := range users {
		if !u.LiveTV && !u.IsAdmin {
			continue
		}
		p, err := s.Store.UserPrefs(u.ID)
		if err != nil || len(p.LiveFollows) == 0 || p.LiveAlertMinutes == 0 {
			continue
		}
		follows := map[string]bool{}
		for _, t := range p.LiveFollows {
			follows[strings.ToLower(strings.TrimSpace(t))] = true
		}
		chans, err := s.Store.Channels(u.ID, false)
		if err != nil {
			continue
		}
		byEPG := map[string]*store.Channel{}
		var ids []string
		for _, c := range chans {
			if c.EPGID != "" && byEPG[c.EPGID] == nil {
				byEPG[c.EPGID] = c
				ids = append(ids, c.EPGID)
			}
		}
		// Starting from now up to the lead time (a minute either side, so a
		// tick that lands just after the start still catches it).
		from, to := now.Add(-time.Minute).Unix(), now.Add(time.Duration(p.LiveAlertMinutes)*time.Minute).Unix()
		progs, err := s.Store.Programs(ids, from, to)
		if err != nil {
			continue
		}
		for _, pr := range progs {
			if pr.Start < from || pr.Start > to || !follows[strings.ToLower(strings.TrimSpace(pr.Title))] {
				continue
			}
			if fresh, err := s.Store.MarkLiveAlert(u.ID, pr.EPGID, pr.Start); err != nil || !fresh {
				continue
			}
			c := byEPG[pr.EPGID]
			mins := int(time.Until(time.Unix(pr.Start, 0)).Round(time.Minute).Minutes())
			n := &store.Notification{UserID: u.ID, Kind: "live", Link: fmt.Sprintf("/live/%d", c.ID), Image: pr.Icon}
			n.Title = fmt.Sprintf("%s starts in %d minutes", pr.Title, mins)
			if mins <= 1 {
				n.Title = pr.Title + " is starting"
			}
			n.Body = strings.TrimPrefix(strings.Join([]string{pr.Subtitle, "On " + c.Name}, " · "), " · ")
			if err := s.Store.AddNotification(n); err != nil {
				slog.Warn("notification", "err", err)
			}
		}
	}
}

// notifyNewEpisodes sends one notification per show and user for episodes
// added since the last run. The first run only sets the watermark, so an
// existing library does not flood everyone.
func (s *Server) notifyNewEpisodes() {
	since, err := strconv.ParseInt(s.Store.Setting(settingNotifyWatermark), 10, 64)
	if err != nil {
		s.Store.SetSettings(map[string]string{settingNotifyWatermark: strconv.FormatInt(time.Now().Unix(), 10)})
		return
	}
	eps, err := s.Store.EpisodesAddedSince(since, 2000)
	if err != nil || len(eps) == 0 {
		return
	}
	byShow := map[int64][]store.NewEpisode{}
	var order []int64
	for _, e := range eps {
		if _, seen := byShow[e.ShowID]; !seen {
			order = append(order, e.ShowID)
		}
		byShow[e.ShowID] = append(byShow[e.ShowID], e)
	}
	prefs := map[int64]bool{}
	wants := func(uid int64) bool {
		v, ok := prefs[uid]
		if !ok {
			p, err := s.Store.UserPrefs(uid)
			v = err == nil && p.NotifyEpisodes
			prefs[uid] = v
		}
		return v
	}
	for _, showID := range order {
		list := byShow[showID]
		users, err := s.Store.ShowFollowers(showID)
		if err != nil {
			continue
		}
		for _, uid := range users {
			if !wants(uid) {
				continue
			}
			show, err := s.Store.Item(uid, showID)
			if err != nil {
				continue // can't see this library
			}
			var fresh []store.NewEpisode
			for _, e := range list {
				if !s.Store.WatchedItem(uid, e.ID) {
					fresh = append(fresh, e)
				}
			}
			if len(fresh) == 0 {
				continue
			}
			n := &store.Notification{UserID: uid, Kind: "episodes", Image: show.Poster, Link: fmt.Sprintf("/item/%d", showID)}
			if len(fresh) == 1 {
				e := fresh[0]
				n.Title = "New episode of " + show.Title
				n.Body = episodeLabel(e)
				n.Link = fmt.Sprintf("/item/%d", e.ID)
			} else {
				n.Title = fmt.Sprintf("%d new episodes of %s", len(fresh), show.Title)
				n.Body = episodeLabel(fresh[0]) + " to " + episodeLabel(fresh[len(fresh)-1])
			}
			if err := s.Store.AddNotification(n); err != nil {
				slog.Warn("notification", "err", err)
			}
		}
	}
	last := eps[len(eps)-1].AddedAt
	s.Store.SetSettings(map[string]string{settingNotifyWatermark: strconv.FormatInt(last, 10)})
}

func episodeLabel(e store.NewEpisode) string {
	label := fmt.Sprintf("S%02dE%02d", e.Season, e.Episode)
	if e.Title != "" {
		label += " · " + e.Title
	}
	return label
}
