package api

import (
	"net/http"
	"strings"

	"bingebandit/internal/store"
)

// Kid mode for Live TV: admins approve channels (or whole groups) for kids,
// and users marked as kids only see those. The filtering itself is in the
// store, so the guide, the channel list, the M3U/XMLTV exports and the Plex
// tuner all follow it.

func (s *Server) kidChannels(w http.ResponseWriter, r *http.Request) {
	groups, ids, err := s.Store.KidApprovals()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "channelIds": ids})
}

func (s *Server) setKidChannels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChannelIDs []int64 `json:"channelIds"`
		On         bool    `json:"on"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetKidChannels(req.ChannelIDs, req.On); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setKidGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		On   bool   `json:"on"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetKidGroup(req.Name, req.On); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setUserKid marks a user (or profile) as a kid. Marking one ends anything
// they're watching, which may not be approved for kids.
func (s *Server) setUserKid(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		On bool `json:"on"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetKid(id, req.On); err != nil {
		writeStoreErr(w, err)
		return
	}
	if req.On {
		s.Live.KickUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// kidTitles lists the movies (?kind=movie) or shows (?kind=show) approved
// for the signed-in kid: their Kids Movies and Kids Shows.
func (s *Server) kidTitles(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "show" {
		writeErr(w, http.StatusBadRequest, "kind must be movie or show")
		return
	}
	items, err := s.Store.KidTitles(currentUser(r).ID, kind)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// kidCandidates are the kids u may approve titles for: an admin, any kid on
// the server; an account, the kid profiles under it.
func (s *Server) kidCandidates(u *store.User) ([]*store.User, error) {
	var users []*store.User
	var err error
	switch {
	case u.IsAdmin:
		users, err = s.Store.ListUsers()
	case u.OwnerID == 0:
		users, err = s.Store.Household(u.ID)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*store.User
	for _, k := range users {
		if k.Kid {
			out = append(out, k)
		}
	}
	return out, nil
}

type kidTitleState struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Approved bool   `json:"approved"`
	Avatar   string `json:"avatar,omitempty"`
}

// approvableItem loads movie or show {id} if the signed-in user can see it.
func (s *Server) approvableItem(w http.ResponseWriter, r *http.Request) *store.Item {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	it, err := s.Store.Item(currentUser(r).ID, id)
	if err == nil && it.Kind != "movie" && it.Kind != "show" {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return it
}

// itemKids lists the kids the signed-in parent or admin can approve this
// movie or show for, and which already have it.
func (s *Server) itemKids(w http.ResponseWriter, r *http.Request) {
	it := s.approvableItem(w, r)
	if it == nil {
		return
	}
	kids, err := s.kidCandidates(currentUser(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	have, err := s.Store.KidTitleUsers(it.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := []kidTitleState{}
	for _, k := range kids {
		out = append(out, kidTitleState{ID: k.ID, Name: memberName(k), Approved: have[k.ID], Avatar: k.Avatar})
	}
	writeJSON(w, http.StatusOK, out)
}

// setItemKids sets which of the caller's kids this movie or show is
// approved for; kids the caller doesn't manage are left alone.
func (s *Server) setItemKids(w http.ResponseWriter, r *http.Request) {
	it := s.approvableItem(w, r)
	if it == nil {
		return
	}
	var req struct {
		UserIDs []int64 `json:"userIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	kids, err := s.kidCandidates(u)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	want := map[int64]bool{}
	for _, id := range req.UserIDs {
		want[id] = true
	}
	for _, k := range kids {
		if err := s.Store.SetKidTitle(k.ID, it.ID, u.ID, want[k.ID]); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
