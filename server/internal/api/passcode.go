package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// A settings passcode keeps Account changes and the Admin pages behind a
// short PIN, so someone using a signed-in browser (a child, a guest) can watch
// but not change anything. Entering it unlocks that one session for a while;
// API keys are for scripts and are never locked.

const unlockFor = 15 * time.Minute

var passcodeFormat = regexp.MustCompile(`^[0-9]{4,8}$`)

// unlocks remembers which session tokens entered their passcode, and until when.
type unlocks struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (u *unlocks) unlocked(token string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	t, ok := u.until[token]
	if ok && time.Now().After(t) {
		delete(u.until, token)
		return false
	}
	return ok
}

// extend starts or renews the unlock, so it lapses after a quiet spell.
func (u *unlocks) extend(token string) time.Time {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.until == nil {
		u.until = map[string]time.Time{}
	}
	t := time.Now().Add(unlockFor)
	u.until[token] = t
	return t
}

func (u *unlocks) lock(token string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.until, token)
}

func (u *unlocks) expiry(token string) time.Time {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.until[token]
}

// guardedPath reports whether a request changes settings: anything under
// /api/admin, account changes under /api/me, and approving a device link.
func guardedPath(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/admin/"):
		return true
	case p == "/api/link/approve":
		return true
	case strings.HasPrefix(p, "/api/household/") && r.Method != http.MethodGet:
		return !strings.HasSuffix(p, "/switch")
	case p == "/api/household" && r.Method != http.MethodGet:
		return true
	case strings.HasPrefix(p, "/api/me/") && r.Method != http.MethodGet && r.Method != http.MethodHead:
		return p != "/api/me/passcode/unlock" && p != "/api/me/passcode/lock"
	}
	return false
}

// settingsLock refuses guarded requests from a locked session with 423, which
// the web UI answers with a passcode prompt.
func (s *Server) settingsLock(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		token := sessionToken(r)
		if u != nil && u.HasPasscode && token != "" && guardedPath(r) {
			if !s.unlocks.unlocked(token) {
				writeJSON(w, http.StatusLocked, map[string]any{"error": "Enter your settings passcode to continue", "locked": true})
				return
			}
			s.unlocks.extend(token)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) passcodeStatus(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	token := sessionToken(r)
	resp := map[string]any{"set": u.HasPasscode, "unlocked": !u.HasPasscode || token == "" || s.unlocks.unlocked(token)}
	if t := s.unlocks.expiry(token); u.HasPasscode && !t.IsZero() {
		resp["unlockedUntil"] = t.Unix()
	}
	writeJSON(w, http.StatusOK, resp)
}

// setPasscode sets or changes the passcode. Changing one needs an unlocked
// session, which settingsLock already checked.
func (s *Server) setPasscode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passcode string `json:"passcode"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !passcodeFormat.MatchString(req.Passcode) {
		writeErr(w, http.StatusBadRequest, "use 4 to 8 digits")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Passcode), bcrypt.DefaultCost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.Store.SetPasscode(currentUser(r).ID, string(hash)); err != nil {
		writeStoreErr(w, err)
		return
	}
	// The person who just chose it shouldn't be asked for it straight away.
	if token := sessionToken(r); token != "" {
		s.unlocks.extend(token)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removePasscode(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetPasscode(currentUser(r).ID, ""); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unlockSettings accepts the passcode, or the account password for anyone
// who forgot it. Failures count against the login limiter.
func (s *Server) unlockSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Passcode string `json:"passcode"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	token := sessionToken(r)
	if !u.HasPasscode || token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"unlocked": true})
		return
	}
	key := "passcode:" + strconv.FormatInt(u.ID, 10) + ":" + clientIP(r)
	if !s.limiter.allow(key) {
		writeErr(w, http.StatusTooManyRequests, "too many wrong tries; wait 15 minutes")
		return
	}
	ok := false
	switch {
	case req.Passcode != "":
		ok = bcrypt.CompareHashAndPassword([]byte(u.PasscodeHash), []byte(req.Passcode)) == nil
	case req.Password != "" && u.HasPassword:
		ok = bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) == nil
	}
	if !ok {
		s.limiter.fail(key)
		// Not 401: that would sign the web UI out.
		writeErr(w, http.StatusForbidden, "That didn't match")
		return
	}
	until := s.unlocks.extend(token)
	writeJSON(w, http.StatusOK, map[string]any{"unlocked": true, "unlockedUntil": until.Unix()})
}

func (s *Server) lockSettings(w http.ResponseWriter, r *http.Request) {
	s.unlocks.lock(sessionToken(r))
	w.WriteHeader(http.StatusNoContent)
}

// clearUserPasscode lets an admin remove a passcode someone forgot.
func (s *Server) clearUserPasscode(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.SetPasscode(id, ""); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setUserLiveTV turns Live TV and the DVR on or off for a user (and its
// profiles). Turning it off also ends anything they're watching.
func (s *Server) setUserLiveTV(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Store.SetLiveTV(id, req.On); err != nil {
		writeStoreErr(w, err)
		return
	}
	if !req.On {
		s.Live.KickUser(id)
	}
	w.WriteHeader(http.StatusNoContent)
}
