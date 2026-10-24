package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/store"
)

func (s *Server) watchlist(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.Watchlist(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) setWatchlist(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			writeErr(w, http.StatusBadRequest, "only movies and shows go on the watchlist")
			return
		}
		if add {
			err = s.Store.AddToWatchlist(uid, id)
		} else {
			err = s.Store.RemoveFromWatchlist(uid, id)
		}
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) playlists(w http.ResponseWriter, r *http.Request) {
	lists, err := s.Store.Playlists(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, lists)
}

// ownPlaylist loads {id} if it belongs to the current user.
func (s *Server) ownPlaylist(w http.ResponseWriter, r *http.Request) *store.Playlist {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	p, err := s.Store.Playlist(currentUser(r).ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return p
}

// playableIDs expands movies/episodes/seasons/shows into episode and movie
// ids the user may see, in watching order.
func (s *Server) playableIDs(uid int64, ids []int64) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if _, err := s.Store.Item(uid, id); err != nil {
			return nil, err
		}
		leaves, err := s.Store.LeafIDs(id)
		if err != nil {
			return nil, err
		}
		out = append(out, leaves...)
	}
	return out, nil
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string  `json:"name"`
		ItemIDs []int64 `json:"itemIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	name := store.NormalizeName(req.Name)
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name the playlist")
		return
	}
	uid := currentUser(r).ID
	ids, err := s.playableIDs(uid, req.ItemIDs)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	p, err := s.Store.CreatePlaylist(uid, name)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if len(ids) > 0 {
		if _, err := s.Store.AddToPlaylist(p.ID, ids); err != nil {
			writeStoreErr(w, err)
			return
		}
		p, _ = s.Store.Playlist(uid, p.ID)
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) playlist(w http.ResponseWriter, r *http.Request) {
	p := s.ownPlaylist(w, r)
	if p == nil {
		return
	}
	items, err := s.Store.PlaylistItems(currentUser(r).ID, p.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlist": p, "items": items})
}

func (s *Server) renamePlaylist(w http.ResponseWriter, r *http.Request) {
	p := s.ownPlaylist(w, r)
	if p == nil {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || store.NormalizeName(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name the playlist")
		return
	}
	if err := s.Store.RenamePlaylist(p.ID, store.NormalizeName(req.Name)); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	if p := s.ownPlaylist(w, r); p != nil {
		if err := s.Store.DeletePlaylist(p.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	p := s.ownPlaylist(w, r)
	if p == nil {
		return
	}
	var req struct {
		ItemIDs []int64 `json:"itemIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ids, err := s.playableIDs(currentUser(r).ID, req.ItemIDs)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	added, err := s.Store.AddToPlaylist(p.ID, ids)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

func (s *Server) removeFromPlaylist(w http.ResponseWriter, r *http.Request) {
	p := s.ownPlaylist(w, r)
	if p == nil {
		return
	}
	item, ok := idParam(r, "item")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad item id")
		return
	}
	if err := s.Store.RemoveFromPlaylist(p.ID, item); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderPlaylist(w http.ResponseWriter, r *http.Request) {
	p := s.ownPlaylist(w, r)
	if p == nil {
		return
	}
	var req struct {
		ItemIDs []int64 `json:"itemIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.ReorderPlaylist(p.ID, req.ItemIDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) playlistNext(w http.ResponseWriter, r *http.Request) {
	p := s.ownPlaylist(w, r)
	if p == nil {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	next, err := s.Store.PlaylistNext(currentUser(r).ID, p.ID, after)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, next)
}

func pageParams(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return limit, max(offset, 0)
}

func (s *Server) myHistory(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	h, err := s.Store.History(currentUser(r).ID, limit, offset)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) setProfileVisibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Public bool `json:"public"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetProfilePublic(currentUser(r).ID, req.Public); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// profile shows a user's stats and recent history to themselves, to admins,
// and to everyone on the server when the owner has made it public.
func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	u, err := s.Store.UserByUsername(chi.URLParam(r, "username"))
	if err == nil && u.ID != me.ID && !me.IsAdmin && !u.ProfilePublic {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	stats, err := s.Store.UserStats(u.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	recent, err := s.Store.History(u.ID, 12, 0)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for i := range recent {
		recent[i].IP = "" // never show IPs on profiles
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":   map[string]any{"username": u.Username, "createdAt": u.CreatedAt, "profilePublic": u.ProfilePublic, "isMe": u.ID == me.ID},
		"stats":  stats,
		"recent": recent,
	})
}

// publicProfiles lists users who share their profile, for discovery.
func (s *Server) publicProfiles(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, u := range users {
		if u.ProfilePublic || u.ID == currentUser(r).ID {
			out = append(out, map[string]any{"username": u.Username, "isMe": u.ID == currentUser(r).ID})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminHistory(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	uid, _ := strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	h, err := s.Store.History(uid, limit, offset)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) adminStatistics(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	st, err := s.Store.ServerStats(days)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
