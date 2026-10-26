package api

import (
	"net/http"
	"strings"
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
