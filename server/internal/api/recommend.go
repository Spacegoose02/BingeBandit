package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"bingebandit/internal/store"
)

func (s *Server) getRecommendOptIn(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"accept": s.Store.AcceptsRecommendations(currentUser(r).ID)})
}

func (s *Server) setRecommendOptIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accept bool `json:"accept"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetAcceptsRecommendations(currentUser(r).ID, req.Accept); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// recommendable returns the movie or show the viewer may recommend.
func (s *Server) recommendable(w http.ResponseWriter, r *http.Request) *store.Item {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	it, err := s.Store.Item(currentUser(r).ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	if it.Kind != "movie" && it.Kind != "show" {
		writeErr(w, http.StatusBadRequest, "only movies and shows can be recommended")
		return nil
	}
	return it
}

// recipients lists who could get this title: people who opted in and can
// see its library.
func (s *Server) recipients(me int64, it *store.Item) ([]store.Recipient, error) {
	all, err := s.Store.RecommendationRecipients(me)
	if err != nil {
		return nil, err
	}
	out := []store.Recipient{}
	for _, rc := range all {
		if _, err := s.Store.Item(rc.ID, it.ID); err == nil {
			out = append(out, rc)
		}
	}
	return out, nil
}

func (s *Server) recommendRecipients(w http.ResponseWriter, r *http.Request) {
	it := s.recommendable(w, r)
	if it == nil {
		return
	}
	out, err := s.recipients(currentUser(r).ID, it)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) recommend(w http.ResponseWriter, r *http.Request) {
	it := s.recommendable(w, r)
	if it == nil {
		return
	}
	var req struct {
		UserIDs []int64 `json:"userIds"`
		Note    string  `json:"note"`
	}
	if err := readJSON(r, &req); err != nil || len(req.UserIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "pick at least one person")
		return
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > 280 {
		writeErr(w, http.StatusBadRequest, "keep the note under 280 characters")
		return
	}
	me := currentUser(r).ID
	allowed, err := s.recipients(me, it)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	ok := map[int64]bool{}
	for _, rc := range allowed {
		ok[rc.ID] = true
	}
	sent := 0
	for _, id := range req.UserIDs {
		if !ok[id] {
			continue // not opted in, or can't see it: skip quietly
		}
		if err := s.Store.Recommend(me, id, it.ID, note); err != nil {
			writeStoreErr(w, err)
			return
		}
		sent++
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent})
}

func (s *Server) dismissRecommendation(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DismissRecommendation(currentUser(r).ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// recommendedRow is the Home row of titles recommended to the viewer that
// they can still see and haven't finished, with who recommended each.
func (s *Server) recommendedRow(uid int64) ([]*store.Item, map[int64]store.Recommendation) {
	recs, err := s.Store.RecommendationsFor(uid, 40)
	if err != nil {
		return nil, nil
	}
	items := []*store.Item{}
	by := map[int64]store.Recommendation{}
	for _, rec := range recs {
		it, err := s.Store.Item(uid, rec.ItemID)
		if err != nil {
			continue
		}
		if (it.Kind == "movie" && it.Watch.Watched) || (it.Kind == "show" && it.ChildCount > 0 && it.UnwatchedCount == 0) {
			continue // already seen
		}
		items = append(items, it)
		by[it.ID] = rec
	}
	return items, by
}
