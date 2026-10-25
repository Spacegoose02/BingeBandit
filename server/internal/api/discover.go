package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"bingebandit/internal/discover"
	"bingebandit/internal/store"
)

// discoverRows lists each Discover source that has videos, newest first.
func (s *Server) discoverRows(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.Store.DiscoverSources()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type row struct {
		ID     int64                 `json:"id"`
		Name   string                `json:"name"`
		Videos []store.DiscoverVideo `json:"videos"`
	}
	out := []row{}
	for _, src := range srcs {
		videos, err := s.Store.DiscoverVideos(src.ID, 30)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if len(videos) > 0 {
			out = append(out, row{src.ID, src.Name, videos})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) discoverSources(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.Store.DiscoverSources()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, srcs)
}

// addDiscoverSource checks that the playlist or channel can be read before
// adding it, so a typo is reported straight away.
func (s *Server) addDiscoverSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	src, err := discover.Parse(ctx, req.URL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	feed, err := discover.Fetch(ctx, src)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = feed.Title
	}
	if name == "" {
		name = "YouTube"
	}
	d := &store.DiscoverSource{Name: limitRunes(name, 80), Kind: src.Kind, YouTubeID: src.ID}
	if err := s.Store.AddDiscoverSource(d); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "that playlist or channel is already on Discover")
			return
		}
		writeStoreErr(w, err)
		return
	}
	videos := make([]store.DiscoverVideo, 0, len(feed.Videos))
	for _, v := range feed.Videos {
		videos = append(videos, store.DiscoverVideo{ID: v.ID, Title: v.Title, Description: v.Description, PublishedAt: v.Published.Unix()})
	}
	if err := s.Store.SaveDiscoverVideos(d.ID, videos, ""); err != nil {
		writeStoreErr(w, err)
		return
	}
	d, _ = s.Store.DiscoverSource(d.ID)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) renameDiscoverSource(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.Store.RenameDiscoverSource(id, limitRunes(strings.TrimSpace(req.Name), 80)); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteDiscoverSource(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteDiscoverSource(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) orderDiscoverSources(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.OrderDiscoverSources(req.IDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshDiscover fetches every source now and returns their status.
func (s *Server) refreshDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if _, err := discover.RefreshAll(ctx, s.Store); errors.Is(err, context.DeadlineExceeded) {
		writeErr(w, http.StatusGatewayTimeout, "YouTube took too long; the rest will refresh in the background task")
		return
	}
	s.discoverSources(w, r)
}

func limitRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
