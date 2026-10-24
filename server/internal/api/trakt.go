package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bingebandit/internal/store"
	"bingebandit/internal/trakt"
)

func (s *Server) traktStatus(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	out := map[string]any{"configured": s.Trakt.Configured(), "account": nil}
	if acc, err := s.Store.TraktAccount(uid); err == nil {
		out["account"] = acc
		out["pendingNew"] = s.Store.CountPendingTrakt(uid, acc.ConnectedAt)
		out["pendingAll"] = s.Store.CountPendingTrakt(uid, 0)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) traktConnect(w http.ResponseWriter, r *http.Request) {
	d, err := s.Trakt.Start(r.Context(), currentUser(r).ID)
	if err != nil {
		writeTraktErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"userCode": d.UserCode, "verificationUrl": d.VerificationURL, "expiresIn": d.ExpiresIn, "interval": d.Interval})
}

// traktPoll: 202 while waiting, 200 with the account once approved.
func (s *Server) traktPoll(w http.ResponseWriter, r *http.Request) {
	acc, err := s.Trakt.Poll(r.Context(), currentUser(r).ID)
	switch {
	case errors.Is(err, trakt.ErrPending), errors.Is(err, trakt.ErrSlowDown):
		w.WriteHeader(http.StatusAccepted)
	case err != nil:
		writeTraktErr(w, err)
	default:
		writeJSON(w, http.StatusOK, acc)
	}
}

func (s *Server) traktDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.Trakt.Disconnect(r.Context(), currentUser(r).ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) traktSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AutoSync bool `json:"autoSync"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetTraktAutoSync(currentUser(r).ID, req.AutoSync); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// traktPush sends history to Trakt: everything not sent yet (all) or only
// plays since connecting.
func (s *Server) traktPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		All bool `json:"all"`
	}
	readJSON(r, &req)
	uid := currentUser(r).ID
	acc, err := s.Store.TraktAccount(uid)
	if err != nil {
		writeErr(w, http.StatusConflict, "connect Trakt first")
		return
	}
	since := acc.ConnectedAt
	if req.All {
		since = 0
	}
	res, err := s.Trakt.Push(r.Context(), uid, since)
	if err != nil {
		writeTraktErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) traktImport(w http.ResponseWriter, r *http.Request) {
	st, err := s.Trakt.Import(r.Context(), currentUser(r).ID)
	if err != nil {
		writeTraktErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func writeTraktErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, trakt.ErrNotConfigured):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, trakt.ErrExpired), errors.Is(err, trakt.ErrDenied):
		writeErr(w, http.StatusGone, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusConflict, "connect Trakt first")
	case trakt.IsAuth(err):
		// Not 401: that would sign the person out of BingeBandit itself.
		writeErr(w, http.StatusConflict, "Trakt no longer accepts this connection; disconnect and connect again")
	default:
		writeErr(w, http.StatusBadGateway, err.Error())
	}
}

// ---- downloads ----

// exportHistory writes the user's plays as CSV: a general format with TMDB
// ids, or Letterboxd's import format (movies only).
func (s *Server) exportHistory(format string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		plays, err := s.Store.WatchedPlays(u.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		name := fmt.Sprintf("bingebandit-%s-%s.csv", u.Username, format)
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		cw := csv.NewWriter(w)
		date := func(t int64) string { return time.Unix(t, 0).UTC().Format("2006-01-02") }
		tmdb := func(id int64) string {
			if id == 0 {
				return ""
			}
			return strconv.FormatInt(id, 10)
		}
		if format == "letterboxd" {
			// https://letterboxd.com/about/importing-data/ — rewatches flagged after the first viewing.
			cw.Write([]string{"tmdbID", "Title", "Year", "WatchedDate", "Rewatch"})
			seen := map[string]bool{}
			for i := len(plays) - 1; i >= 0; i-- { // oldest first so the first viewing isn't a rewatch
				p := plays[i]
				if p.Kind != "movie" || !p.Completed {
					continue
				}
				key := p.Title + "|" + strconv.Itoa(p.Year)
				cw.Write([]string{tmdb(p.TMDBID), p.Title, yearStr(p.Year), date(p.WatchedAt), strconv.FormatBool(seen[key])})
				seen[key] = true
			}
		} else {
			cw.Write([]string{"watched_at", "type", "title", "year", "tmdb_id", "season", "episode", "episode_title", "completed"})
			for _, p := range plays {
				season, ep := "", ""
				if p.Kind == "episode" {
					season, ep = strconv.Itoa(p.Season), strconv.Itoa(p.Episode)
				}
				cw.Write([]string{time.Unix(p.WatchedAt, 0).UTC().Format(time.RFC3339), p.Kind, p.Title, yearStr(p.Year), tmdb(p.TMDBID),
					season, ep, p.EpisodeTitle, strconv.FormatBool(p.Completed)})
			}
		}
		cw.Flush()
	}
}

func yearStr(y int) string {
	if y == 0 {
		return ""
	}
	return strconv.Itoa(y)
}
