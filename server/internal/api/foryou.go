package api

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

// Recommendations from the server's own library: "Because you watched" and
// "Because you liked" rows on Home and "More like this" on a title's page.
// TMDB's recommendations (cached for an hour) say what's alike; only titles
// the viewer can play are kept. Without TMDB (no key, or no internet),
// "More like this" falls back to titles that share the most genres.

// tmdbBudget bounds how long Home and a title's page wait for TMDB, so a
// slow or unreachable TMDB never holds them up.
const tmdbBudget = 2500 * time.Millisecond

func (s *Server) tmdbOn() bool { return s.Metadata != nil && s.Metadata.Enabled() }

// seen reports whether the viewer has finished a movie or every episode of a show.
func seen(it *store.Item) bool {
	return (it.Kind == "movie" && it.Watch.Watched) || (it.Kind == "show" && it.ChildCount > 0 && it.UnwatchedCount == 0)
}

// ownedLike turns TMDB's recommendations into the titles the viewer can
// play, leaving out skip (the title itself), thumbs-down titles and, with
// unseenOnly, what they've finished.
func (s *Server) ownedLike(uid int64, list []metadata.Requestable, skip int64, disliked map[int64]bool, unseenOnly bool, limit int) []*store.Item {
	out := []*store.Item{}
	have := map[int64]bool{skip: true}
	for _, rq := range list {
		if len(out) >= limit {
			break
		}
		items, err := s.Store.FindItems(uid, rq.Kind, rq.TMDBID, "", 0)
		if err != nil || len(items) == 0 {
			continue
		}
		it := items[0]
		if have[it.ID] || disliked[it.ID] || (unseenOnly && seen(it)) {
			continue
		}
		have[it.ID] = true
		out = append(out, it)
	}
	return out
}

// becauseRows are Home's "Because you watched …" (the two titles the viewer
// finished last) and "Because you liked …" (their latest thumbs up) rows.
func (s *Server) becauseRows(ctx context.Context, uid int64) []homeRow {
	if !s.tmdbOn() || s.Store.Setting(store.SettingBecauseRows) == "false" {
		return nil
	}
	type src struct {
		id, title string
		f         store.FinishedTitle
	}
	var srcs []src
	used := map[int64]bool{}
	for _, f := range s.Store.RecentlyFinished(uid, 2) {
		used[f.TMDBID] = true
		srcs = append(srcs, src{"because-watched-" + f.Kind, "Because you watched " + f.Title, f})
	}
	for _, f := range s.Store.RecentlyLiked(uid, 3) {
		if used[f.TMDBID] {
			continue
		}
		srcs = append(srcs, src{"because-liked", "Because you liked " + f.Title, f})
		break
	}
	if len(srcs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, tmdbBudget)
	defer cancel()
	disliked := s.Store.Disliked(uid)
	rows := make([]homeRow, len(srcs))
	var wg sync.WaitGroup
	for i, sr := range srcs {
		wg.Go(func() {
			list, err := s.Metadata.Recommendations(ctx, sr.f.Kind, sr.f.TMDBID)
			if err != nil {
				return
			}
			self := int64(0)
			if mine, _ := s.Store.FindItems(uid, sr.f.Kind, sr.f.TMDBID, "", 0); len(mine) > 0 {
				self = mine[0].ID
			}
			if items := s.ownedLike(uid, list, self, disliked, true, 20); len(items) >= 3 {
				rows[i] = homeRow{ID: sr.id + "-" + strconv.FormatInt(sr.f.TMDBID, 10), Title: sr.title, Items: items}
			}
		})
	}
	wg.Wait()
	out := []homeRow{}
	for _, r := range rows {
		if r.ID != "" {
			out = append(out, r)
		}
	}
	return out
}

// similarItems is "More like this" for a movie or show: what TMDB says is
// alike that the viewer can play, topped up with titles sharing its genres.
func (s *Server) similarItems(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	uid := currentUser(r).ID
	it, err := s.Store.Item(uid, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	const limit = 20
	out := []*store.Item{}
	if it.Kind != "movie" && it.Kind != "show" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	disliked := s.Store.Disliked(uid)
	if s.tmdbOn() && it.TMDBID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), tmdbBudget)
		if list, err := s.Metadata.Recommendations(ctx, it.Kind, it.TMDBID); err == nil {
			out = s.ownedLike(uid, list, it.ID, disliked, false, limit)
		}
		cancel()
	}
	if len(out) < limit {
		have := map[int64]bool{}
		for _, x := range out {
			have[x.ID] = true
		}
		if more, err := s.Store.SimilarByGenre(uid, it, limit); err == nil {
			for _, x := range more {
				if len(out) >= limit {
					break
				}
				if !have[x.ID] {
					out = append(out, x)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// rateItem saves the viewer's thumbs on a movie or show: -1 (not for me,
// also "Not interested" on Home), 1 (like), 2 (love) or 0 to clear it.
func (s *Server) rateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	uid := currentUser(r).ID
	it, err := s.Store.Item(uid, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if it.Kind != "movie" && it.Kind != "show" {
		writeErr(w, http.StatusBadRequest, "only movies and shows take a rating")
		return
	}
	var req struct {
		Rating int `json:"rating"`
	}
	if err := readJSON(r, &req); err != nil || req.Rating < -1 || req.Rating > 2 {
		writeErr(w, http.StatusBadRequest, "rating is -1, 0, 1 or 2")
		return
	}
	if err := s.Store.SetTitleRating(uid, it.ID, req.Rating); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"rating": req.Rating})
}
