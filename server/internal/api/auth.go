package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

const cookieName = "bb_session"

type ctxKey int

const userKey ctxKey = 1

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// requireAuth accepts a bearer token (apps), the session cookie (web UI), or a
// ?token= query parameter on GET (players that cannot set headers).
// Cookie-authenticated writes must carry X-Requested-With to block CSRF.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, viaCookie := "", false
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if c, err := r.Cookie(cookieName); err == nil {
			token, viaCookie = c.Value, true
		} else if r.Method == http.MethodGet {
			token = r.URL.Query().Get("token")
		}
		u, err := s.Store.UserBySession(token)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if viaCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "BingeBandit" {
			writeErr(w, http.StatusForbidden, "missing X-Requested-With header")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := currentUser(r); u == nil || !u.IsAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"needsSetup": n == 0,
		"serverName": s.Store.Setting(store.SettingServerName),
		"version":    s.Version,
	})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Device   string `json:"device"`
}

func validCredentials(c credentials) error {
	c.Username = strings.TrimSpace(c.Username)
	if len(c.Username) < 2 || len(c.Username) > 64 {
		return errors.New("username must be 2-64 characters")
	}
	if len(c.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

// setup creates the first (admin) account. It only works while no users exist.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := validCredentials(c); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.Store.CountUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if n > 0 {
		writeErr(w, http.StatusConflict, "server is already set up")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	u, err := s.Store.CreateUser(strings.TrimSpace(c.Username), string(hash), true)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.startSession(w, r, u, c.Device)
}

// A real hash to compare against when the username does not exist, so
// response timing does not reveal which usernames are valid.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("bingebandit-timing"), bcrypt.DefaultCost)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, err := s.Store.UserByUsername(strings.TrimSpace(c.Username))
	hash := dummyHash
	if err == nil {
		hash = []byte(u.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(c.Password)) != nil || err != nil {
		s.limiter.fail(ip)
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	s.startSession(w, r, u, c.Device)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User, device string) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	if device == "" {
		device = r.UserAgent()
	}
	if err := s.Store.CreateSession(token, u.ID, device, clientIP(r)); err != nil {
		writeStoreErr(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil, MaxAge: 365 * 24 * 3600,
	})
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		s.Store.DeleteSession(strings.TrimPrefix(h, "Bearer "))
	} else if c, err := r.Cookie(cookieName); err == nil {
		s.Store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	p, err := s.Store.UserPrefs(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request) {
	p := store.DefaultPrefs
	if err := readJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := p.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SetUserPrefs(currentUser(r).ID, p); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
		writeErr(w, http.StatusUnauthorized, "current password is wrong")
		return
	}
	if len(req.New) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err := s.Store.SetPassword(u.ID, string(hash)); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loginLimiter allows 10 failed logins per IP per 15 minutes.
type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{failures: map[string][]time.Time{}} }

const (
	loginWindow   = 15 * time.Minute
	loginMaxFails = 10
)

func (l *loginLimiter) recent(ip string) []time.Time {
	cutoff := time.Now().Add(-loginWindow)
	kept := l.failures[ip][:0]
	for _, t := range l.failures[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, ip)
	} else {
		l.failures[ip] = kept
	}
	return kept
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip)) < loginMaxFails
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[ip] = append(l.recent(ip), time.Now())
}
