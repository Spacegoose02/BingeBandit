package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"bingebandit/internal/store"
)

// Screen time: a parent (the account a profile belongs to) or an admin
// gives a profile a daily allowance and a bedtime. Watching counts while
// a player reports itself playing (films, shows, Live TV); once the
// allowance is used, or at bedtime, starting playback is refused and
// players that are going are told to stop. Admins have no screen time.

// screenTimeNow is the clock; a variable so tests can move it.
var screenTimeNow = time.Now

// screenClocks remembers when each player (a playback session or a Live
// TV viewer) last reported, to count the time in between.
var screenClocks sync.Map // string -> int64 unix seconds

// screenTimeTick counts the time since a player's last report toward its
// user's day. Gaps longer than a couple of report intervals (a player that
// was gone) count as one interval.
func (s *Server) screenTimeTick(u *store.User, key string, playing bool) {
	now := screenTimeNow().Unix()
	last, had := screenClocks.Swap(key, now)
	if !playing || !had || u.IsAdmin {
		return
	}
	d := now - last.(int64)
	if d > 30 {
		d = 30
	}
	if rules, err := s.Store.ScreenTimeRules(u.ID); err == nil && rules.Any() {
		s.Store.AddScreenTime(u.ID, store.ScreenTimeDay(screenTimeNow()), d)
	}
}

func screenTimeForget(key string) { screenClocks.Delete(key) }

type screenTimeState struct {
	Rules       store.ScreenTime `json:"rules"`
	UsedMinutes int              `json:"usedMinutes"`
	LeftMinutes int              `json:"leftMinutes"` // -1: no limit today
	Blocked     string           `json:"blocked,omitempty"`
}

func (s *Server) screenTimeState(u *store.User) screenTimeState {
	st := screenTimeState{LeftMinutes: -1}
	if u.IsAdmin {
		return st
	}
	rules, err := s.Store.ScreenTimeRules(u.ID)
	if err != nil || !rules.Any() {
		return st
	}
	now := screenTimeNow()
	st.Rules = rules
	used, _ := s.Store.ScreenTimeUsed(u.ID, store.ScreenTimeDay(now))
	st.UsedMinutes = int(used / 60)
	if allow := rules.Allowance(now); allow > 0 {
		st.LeftMinutes = max(0, allow-int(used/60))
		if st.LeftMinutes == 0 {
			st.Blocked = "Screen time is up for today."
		}
	}
	if rules.InBedtime(now) {
		st.Blocked = fmt.Sprintf("Screen time: it's bedtime. Watching opens again at %s.", rules.Wake)
	}
	return st
}

// screenTimeBlocked is the message for a user who may not watch now, or "".
// Messages start with "Screen time" so players can show them as they are.
func (s *Server) screenTimeBlocked(u *store.User) string {
	return s.screenTimeState(u).Blocked
}

// myScreenTime is the signed-in profile's allowance and what's left today.
func (s *Server) myScreenTime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.screenTimeState(currentUser(r)))
}

// screenTimeReq sets a profile's rules; "reset" clears today's count (more
// time today).
type screenTimeReq struct {
	store.ScreenTime
	Reset bool `json:"reset"`
}

func (s *Server) applyScreenTime(w http.ResponseWriter, target *store.User, req screenTimeReq) {
	if target.IsAdmin {
		writeErr(w, http.StatusBadRequest, "admins have no screen time limits")
		return
	}
	if err := s.Store.SetScreenTimeRules(target.ID, req.ScreenTime); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Reset {
		s.Store.ResetScreenTime(target.ID, store.ScreenTimeDay(screenTimeNow()))
	}
	writeJSON(w, http.StatusOK, s.screenTimeState(target))
}

// profileScreenTime: the account's own profiles (GET and PUT /household/{id}/screen-time).
func (s *Server) profileScreenTime(w http.ResponseWriter, r *http.Request) {
	_, p := s.ownProfile(w, r)
	if p == nil {
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.screenTimeState(p))
		return
	}
	var req screenTimeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.applyScreenTime(w, p, req)
}

// adminScreenTime: any user or profile (GET and PUT /admin/users/{id}/screen-time).
func (s *Server) adminScreenTime(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, err := s.Store.UserByID(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.screenTimeState(u))
		return
	}
	var req screenTimeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	s.applyScreenTime(w, u, req)
}
