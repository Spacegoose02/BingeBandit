package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/store"
)

// Watch Together: a room holds what is playing, where and whether it is
// paused. Everyone in it can play, pause, seek or move to the next episode;
// each change is sent to the others over a server-sent event stream. Rooms
// live in memory and end once nobody has been connected for a while.

const (
	togetherIdle    = 10 * time.Minute // a room with nobody connected this long is removed
	togetherInvites = 6 * time.Hour    // invitations expire
	maxRoomMembers  = 20
)

// TogetherState is where a room's playback is. Position is in seconds at At
// (Unix milliseconds); while playing, it moves on from there in real time.
type TogetherState struct {
	ItemID   int64   `json:"itemId"`
	Paused   bool    `json:"paused"`
	Position float64 `json:"position"`
	At       int64   `json:"at"`
	Rev      int64   `json:"rev"`
	By       int64   `json:"by"`
	ByName   string  `json:"byName,omitempty"`
}

type TogetherMember struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

type togetherRoom struct {
	id     string
	hostID int64

	mu        sync.Mutex
	state     TogetherState
	conns     map[chan []byte]TogetherMember
	idleSince time.Time
	invited   map[int64]time.Time // user id → when
	inviter   string
	// polled are viewers who read the room instead of streaming it (the TV
	// app: React Native has no EventSource), by user id, with their last look.
	polled map[int64]polledMember
}

type polledMember struct {
	TogetherMember
	at time.Time
}

// pollGone is how long after its last look a polling viewer counts as left.
const pollGone = 10 * time.Second

func (rm *togetherRoom) members() []TogetherMember {
	seen := map[int64]bool{}
	out := []TogetherMember{}
	for _, m := range rm.conns {
		if !seen[m.UserID] {
			seen[m.UserID] = true
			out = append(out, m)
		}
	}
	for id, m := range rm.polled {
		if !seen[id] && time.Since(m.at) < pollGone {
			seen[id] = true
			out = append(out, m.TogetherMember)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// snapshot is what clients get: state, members and the server's clock, so
// they can work out the position now without trusting their own clock.
func (rm *togetherRoom) snapshot() map[string]any {
	return map[string]any{"id": rm.id, "host": rm.hostID, "state": rm.state, "members": rm.members(), "now": time.Now().UnixMilli()}
}

// broadcast sends the room as it is now to every connection. Call with mu held.
func (rm *togetherRoom) broadcast() {
	b, _ := json.Marshal(rm.snapshot())
	for c := range rm.conns {
		select {
		case c <- b:
		default: // a slow client gets the next one
		}
	}
}

type togetherRooms struct {
	mu    sync.Mutex
	rooms map[string]*togetherRoom
}

func (t *togetherRooms) get(id string) *togetherRoom {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	return t.rooms[id]
}

func (t *togetherRooms) add(rm *togetherRoom) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.rooms == nil {
		t.rooms = map[string]*togetherRoom{}
	}
	t.sweep()
	t.rooms[rm.id] = rm
}

func (t *togetherRooms) all() []*togetherRoom {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweep()
	out := make([]*togetherRoom, 0, len(t.rooms))
	for _, rm := range t.rooms {
		out = append(out, rm)
	}
	return out
}

// sweep drops rooms nobody has been in for a while. Call with mu held.
func (t *togetherRooms) sweep() {
	for id, rm := range t.rooms {
		rm.mu.Lock()
		idle := len(rm.conns) == 0 && time.Since(rm.idleSince) > togetherIdle
		rm.mu.Unlock()
		if idle {
			delete(t.rooms, id)
		}
	}
}

func displayName(u *store.User) string { return memberName(u) }

// startTogether opens a room for an item the host can see.
func (s *Server) startTogether(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		ItemID   int64   `json:"itemId"`
		Position float64 `json:"position"`
		Paused   bool    `json:"paused"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !s.playableItem(w, u, req.ItemID) {
		return
	}
	rm := &togetherRoom{
		id: randHex(10), hostID: u.ID, conns: map[chan []byte]TogetherMember{}, idleSince: time.Now(),
		invited: map[int64]time.Time{}, inviter: displayName(u),
		state: TogetherState{ItemID: req.ItemID, Paused: req.Paused, Position: max(req.Position, 0), At: time.Now().UnixMilli(), Rev: 1, By: u.ID, ByName: displayName(u)},
	}
	s.together.add(rm)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	writeJSON(w, http.StatusCreated, rm.snapshot())
}

// playableItem checks the user can see a movie or episode.
func (s *Server) playableItem(w http.ResponseWriter, u *store.User, itemID int64) bool {
	it, err := s.Store.Item(u.ID, itemID)
	if err != nil {
		writeStoreErr(w, err)
		return false
	}
	if it.Kind != "movie" && it.Kind != "episode" {
		writeErr(w, http.StatusBadRequest, "pick a movie or an episode")
		return false
	}
	return true
}

// room loads {id}; anyone with the link may look, but only people who can
// see what is playing get past here.
func (s *Server) room(w http.ResponseWriter, r *http.Request) *togetherRoom {
	rm := s.together.get(chi.URLParam(r, "id"))
	if rm == nil {
		writeErr(w, http.StatusNotFound, "this watch party has ended")
		return nil
	}
	rm.mu.Lock()
	item := rm.state.ItemID
	rm.mu.Unlock()
	if _, err := s.Store.Item(currentUser(r).ID, item); err != nil {
		writeErr(w, http.StatusNotFound, "this watch party is playing something you can't watch")
		return nil
	}
	return rm
}

// getTogether is the room as it is now. With ?watching=1 the viewer is in
// it, polling instead of streaming: they count as a member (and keep the
// room open) while they keep looking.
func (s *Server) getTogether(w http.ResponseWriter, r *http.Request) {
	rm := s.room(w, r)
	if rm == nil {
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if r.URL.Query().Get("watching") == "1" {
		u := currentUser(r)
		if rm.polled == nil {
			rm.polled = map[int64]polledMember{}
		}
		prev, ok := rm.polled[u.ID]
		rm.polled[u.ID] = polledMember{TogetherMember{UserID: u.ID, Name: displayName(u)}, time.Now()}
		delete(rm.invited, u.ID)
		rm.idleSince = time.Now()
		if !ok || time.Since(prev.at) >= pollGone {
			rm.broadcast() // someone joined
		}
	}
	writeJSON(w, http.StatusOK, rm.snapshot())
}

// updateTogether records a play, pause, seek or a move to another item.
func (s *Server) updateTogether(w http.ResponseWriter, r *http.Request) {
	rm := s.room(w, r)
	if rm == nil {
		return
	}
	u := currentUser(r)
	var req struct {
		ItemID   int64   `json:"itemId"`
		Paused   bool    `json:"paused"`
		Position float64 `json:"position"`
	}
	if err := readJSON(r, &req); err != nil || req.Position < 0 {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rm.mu.Lock()
	item := rm.state.ItemID
	rm.mu.Unlock()
	if req.ItemID != 0 && req.ItemID != item && !s.playableItem(w, u, req.ItemID) {
		return
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if req.ItemID == 0 {
		req.ItemID = rm.state.ItemID
	}
	rm.state = TogetherState{ItemID: req.ItemID, Paused: req.Paused, Position: req.Position, At: time.Now().UnixMilli(),
		Rev: rm.state.Rev + 1, By: u.ID, ByName: displayName(u)}
	rm.broadcast()
	writeJSON(w, http.StatusOK, rm.snapshot())
}

// togetherEvents streams the room to one viewer until they leave.
func (s *Server) togetherEvents(w http.ResponseWriter, r *http.Request) {
	rm := s.room(w, r)
	if rm == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming isn't supported here")
		return
	}
	u := currentUser(r)
	ch := make(chan []byte, 8)
	rm.mu.Lock()
	if len(rm.conns) >= maxRoomMembers*2 {
		rm.mu.Unlock()
		writeErr(w, http.StatusConflict, "this watch party is full")
		return
	}
	rm.conns[ch] = TogetherMember{UserID: u.ID, Name: displayName(u)}
	delete(rm.invited, u.ID)
	rm.broadcast()
	rm.mu.Unlock()
	defer func() {
		rm.mu.Lock()
		delete(rm.conns, ch)
		if len(rm.conns) == 0 {
			rm.idleSince = time.Now()
		}
		rm.broadcast()
		rm.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: don't hold events back
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case b := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// inviteTogether asks people to join; they see it in the web app.
func (s *Server) inviteTogether(w http.ResponseWriter, r *http.Request) {
	rm := s.room(w, r)
	if rm == nil {
		return
	}
	var req struct {
		UserIDs []int64 `json:"userIds"`
	}
	if err := readJSON(r, &req); err != nil || len(req.UserIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "pick at least one person")
		return
	}
	allowed, err := s.togetherInvitees(currentUser(r).ID, rm)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	ok := map[int64]bool{}
	for _, p := range allowed {
		ok[p.ID] = true
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for _, id := range req.UserIDs {
		if !ok[id] {
			writeErr(w, http.StatusBadRequest, "one of those people can't be invited")
			return
		}
	}
	for _, id := range req.UserIDs {
		rm.invited[id] = time.Now()
	}
	rm.inviter = displayName(currentUser(r))
	w.WriteHeader(http.StatusNoContent)
}

// togetherInvitees are the people who can be asked to join: anyone who can
// play what's on and hasn't turned watch together invitations off. A kid
// can only ask other kids.
func (s *Server) togetherInvitees(me int64, rm *togetherRoom) ([]store.Recipient, error) {
	rm.mu.Lock()
	item := rm.state.ItemID
	rm.mu.Unlock()
	if _, err := s.Store.Item(me, item); err != nil {
		return nil, err
	}
	self, err := s.Store.UserByID(me)
	if err != nil {
		return nil, err
	}
	users, err := s.Store.ListUsers()
	if err != nil {
		return nil, err
	}
	out := []store.Recipient{}
	for _, u := range users {
		if u.ID == me || (self.Kid && !u.Kid) {
			continue
		}
		if p, err := s.Store.UserPrefs(u.ID); err != nil || !p.NotifyTogether {
			continue
		}
		if _, err := s.Store.Item(u.ID, item); err != nil {
			continue // can't see that library
		}
		out = append(out, store.Recipient{ID: u.ID, Name: displayName(u)})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *Server) togetherPeople(w http.ResponseWriter, r *http.Request) {
	rm := s.room(w, r)
	if rm == nil {
		return
	}
	out, err := s.togetherInvitees(currentUser(r).ID, rm)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// myTogetherInvites lists open invitations to rooms that are still going.
func (s *Server) myTogetherInvites(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	type invite struct {
		Room   string `json:"room"`
		ItemID int64  `json:"itemId"`
		Title  string `json:"title"`
		From   string `json:"from"`
	}
	out := []invite{}
	for _, rm := range s.together.all() {
		rm.mu.Lock()
		at, ok := rm.invited[u.ID]
		if ok && time.Since(at) > togetherInvites {
			delete(rm.invited, u.ID)
			ok = false
		}
		item, from := rm.state.ItemID, rm.inviter
		rm.mu.Unlock()
		if !ok {
			continue
		}
		it, err := s.Store.Item(u.ID, item)
		if err != nil {
			continue
		}
		title := it.Title
		if it.Kind == "episode" && it.ShowTitle != "" && it.SeasonNumber != nil && it.EpisodeNumber != nil {
			title = fmt.Sprintf("%s · S%dE%d", it.ShowTitle, *it.SeasonNumber, *it.EpisodeNumber)
		}
		out = append(out, invite{Room: rm.id, ItemID: item, Title: title, From: from})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) declineTogether(w http.ResponseWriter, r *http.Request) {
	if rm := s.together.get(chi.URLParam(r, "id")); rm != nil {
		rm.mu.Lock()
		delete(rm.invited, currentUser(r).ID)
		rm.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}
