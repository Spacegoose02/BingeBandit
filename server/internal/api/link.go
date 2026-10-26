package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Linking a TV: the TV asks for a short code and shows it; someone signed
// in on a phone or computer enters it at /link; the TV's next poll receives a
// session. Nobody types a password on a remote.

const (
	linkTTL      = 10 * time.Minute
	linkAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O, 1/I/L
)

type pendingLink struct {
	code      string
	pollToken string
	device    string
	expires   time.Time
	userID    int64 // set when approved
	profile   bool  // approved as a profile: the TV opens as it without asking who's watching
}

type linkManager struct {
	mu     sync.Mutex
	byCode map[string]*pendingLink
	byPoll map[string]*pendingLink
}

func newLinkManager() *linkManager {
	return &linkManager{byCode: map[string]*pendingLink{}, byPoll: map[string]*pendingLink{}}
}

func (l *linkManager) prune() {
	now := time.Now()
	for c, p := range l.byCode {
		if now.After(p.expires) {
			delete(l.byCode, c)
			delete(l.byPoll, p.pollToken)
		}
	}
}

func normalizeCode(c string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(c)))
}

func (l *linkManager) start(device string) *pendingLink {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	b := make([]byte, 6)
	var code string
	for {
		rand.Read(b)
		var sb strings.Builder
		for _, c := range b {
			sb.WriteByte(linkAlphabet[int(c)%len(linkAlphabet)])
		}
		code = sb.String()
		if l.byCode[code] == nil {
			break
		}
	}
	tok := make([]byte, 24)
	rand.Read(tok)
	p := &pendingLink{code: code, pollToken: hex.EncodeToString(tok), device: device, expires: time.Now().Add(linkTTL)}
	l.byCode[code] = p
	l.byPoll[p.pollToken] = p
	return p
}

func (s *Server) linkStart(w http.ResponseWriter, r *http.Request) {
	if !s.linkLimiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many link codes requested; wait a few minutes")
		return
	}
	s.linkLimiter.fail(clientIP(r)) // every start counts toward the limit
	var req struct {
		Device string `json:"device"`
	}
	readJSON(r, &req)
	device := strings.TrimSpace(req.Device)
	if device == "" {
		device = "TV"
	}
	if len(device) > 60 {
		device = device[:60]
	}
	p := s.links.start(device)
	writeJSON(w, http.StatusOK, map[string]any{
		"code": p.code, "pollToken": p.pollToken, "expiresIn": int(linkTTL.Seconds()),
		"linkUrl": baseURL(r) + "/link?code=" + p.code,
	})
}

// linkPoll returns 202 while waiting, then the session once (200), or 410.
// With ?web=1 the session is also set as a cookie (for TV web browsers).
func (s *Server) linkPoll(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	s.links.mu.Lock()
	s.links.prune()
	p := s.links.byPoll[tok]
	approved := p != nil && p.userID != 0
	if approved {
		delete(s.links.byPoll, p.pollToken)
		delete(s.links.byCode, p.code)
	}
	s.links.mu.Unlock()
	switch {
	case p == nil:
		writeErr(w, http.StatusGone, "this code expired; get a new one")
	case !approved:
		writeJSON(w, http.StatusAccepted, map[string]bool{"waiting": true})
	default:
		u, err := s.Store.UserByID(p.userID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		token, err := s.newSession(r, u, p.device)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		// Apps take the token from the body; a TV's web browser gets the cookie.
		if r.URL.Query().Get("web") == "1" {
			s.setSessionCookie(w, r, token)
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u, "profileChosen": p.profile})
	}
}

// linkInfo tells the /link page which address to type on a TV that isn't
// on the home network: the public URL when one is set, else this one.
func (s *Server) linkInfo(w http.ResponseWriter, r *http.Request) {
	addr := s.policy().publicURL
	if addr == "" {
		addr = baseURL(r)
	}
	writeJSON(w, http.StatusOK, map[string]string{"serverUrl": addr})
}

// linkApprove signs the device in as the current user, as one of the
// account's profiles (someone's own TV in the kids' room), or — for admins —
// as anyone (to set up a family member's TV). Picking a profile makes the TV
// open as it; picking an account keeps the usual "Who's watching?".
func (s *Server) linkApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code   string `json:"code"`
		UserID int64  `json:"userId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	me := currentUser(r)
	target := me
	if req.UserID != 0 && req.UserID != me.ID {
		t, err := s.Store.UserByID(req.UserID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		// An account may hand a TV to its own profiles. A profile can't pick
		// the account or a sibling: that would skip their PINs.
		ownProfile := me.OwnerID == 0 && t.OwnerID == me.ID
		if !me.IsAdmin && !ownProfile {
			writeErr(w, http.StatusForbidden, "you can link a TV for yourself or your profiles; ask an admin to set one up for someone else")
			return
		}
		target = t
	}
	s.links.mu.Lock()
	s.links.prune()
	p := s.links.byCode[normalizeCode(req.Code)]
	if p != nil {
		p.userID = target.ID
		p.profile = target.OwnerID != 0
	}
	s.links.mu.Unlock()
	if p == nil {
		writeErr(w, http.StatusNotFound, "that code isn't valid or has expired; check the TV for the current one")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"device": p.device, "name": memberName(target)})
}
