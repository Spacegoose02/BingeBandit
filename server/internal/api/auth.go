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

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

const cookieName = "bb_session"

type ctxKey int

const (
	userKey ctxKey = iota + 1
	sessionKey
)

// sessionToken is the session the request signed in with, or "" for an API key.
func sessionToken(r *http.Request) string {
	t, _ := r.Context().Value(sessionKey).(string)
	return t
}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

// requireLiveTV turns away users an admin has turned Live TV off for.
func requireLiveTV(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := currentUser(r); u == nil || !u.LiveTV {
			writeErr(w, http.StatusForbidden, "Live TV is turned off for your account")
			return
		}
		next.ServeHTTP(w, r)
	})
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
		session := token
		if err != nil {
			// API keys work wherever a session token does (apps, scripts, playlist links).
			u, err = s.Store.UserByAPIKey(token)
			session = ""
		}
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "not signed in")
			return
		}
		if viaCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "BingeBandit" {
			writeErr(w, http.StatusForbidden, "missing X-Requested-With header")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessionKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
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
	Code     string `json:"code"` // authenticator or recovery code, when 2FA is on
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
	if u.TOTPEnabled {
		code := strings.TrimSpace(c.Code)
		if code == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Enter the 6-digit code from your authenticator app", "needsCode": true})
			return
		}
		if !validTOTP(u.TOTPSecret, code, time.Now()) && !s.Store.UseRecoveryCode(u.ID, code) {
			s.limiter.fail(ip)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "That code didn't work. Try the newest one from your app", "needsCode": true})
			return
		}
	}
	s.startSession(w, r, u, c.Device)
}

// ---- two-factor setup ----

func (s *Server) setup2FA(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.OwnerID != 0 {
		writeErr(w, http.StatusForbidden, "profiles sign in through their account")
		return
	}
	if u.TOTPEnabled {
		writeErr(w, http.StatusConflict, "two-factor sign-in is already on")
		return
	}
	secret := newTOTPSecret()
	if err := s.Store.SetTOTPSecret(u.ID, secret); err != nil {
		writeStoreErr(w, err)
		return
	}
	issuer := s.Store.Setting(store.SettingServerName)
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": totpURI(secret, u.Username, issuer)})
}

func (s *Server) enable2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	if u.TOTPSecret == "" || !validTOTP(u.TOTPSecret, req.Code, time.Now()) {
		writeErr(w, http.StatusBadRequest, "that code didn't match; check your phone's clock and try the newest code")
		return
	}
	codes := newRecoveryCodes(8)
	if err := s.Store.EnableTOTP(u.ID, codes); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (s *Server) disable2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		writeErr(w, http.StatusUnauthorized, "wrong password")
		return
	}
	if err := s.Store.DisableTOTP(u.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminReset2FA turns off 2FA for a user who lost their phone and codes.
func (s *Server) adminReset2FA(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DisableTOTP(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- devices and API keys ----

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Sessions(currentUser(r).ID, requestToken(r))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.RevokeSession(currentUser(r).ID, chi.URLParam(r, "id")); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) apiKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.APIKeys(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || store.NormalizeName(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name the key so you know where it's used")
		return
	}
	key, k, err := s.Store.CreateAPIKey(currentUser(r).ID, store.NormalizeName(req.Name))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "info": k})
}

func (s *Server) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteAPIKey(currentUser(r).ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setShareActivity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Share bool `json:"share"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetShareActivity(currentUser(r).ID, req.Share); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// friendsActivity shows what people who opted in are watching right now
// (never IPs or devices), skipping items the viewer can't access.
func (s *Server) friendsActivity(w http.ResponseWriter, r *http.Request) {
	me := currentUser(r)
	sharing := map[int64]bool{}
	if users, err := s.Store.ListUsers(); err == nil {
		for _, u := range users {
			sharing[u.ID] = u.ShareActivity && u.ID != me.ID
		}
	}
	type friend struct {
		Username string  `json:"username"`
		ItemID   int64   `json:"itemId"`
		Title    string  `json:"title"`
		Subtitle string  `json:"subtitle,omitempty"`
		Poster   string  `json:"poster,omitempty"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
		Paused   bool    `json:"paused"`
	}
	out := []friend{}
	seen := map[int64]bool{}
	for _, np := range s.Activity.List() {
		if !sharing[np.UserID] || seen[np.UserID] {
			continue
		}
		if _, err := s.Store.Item(me.ID, np.ItemID); err != nil {
			continue
		}
		seen[np.UserID] = true
		out = append(out, friend{np.Username, np.ItemID, np.Title, np.Subtitle, np.Poster, np.Position, np.Duration, np.Paused})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u *store.User, device string) {
	s.startSessionFor(w, r, u, device, true)
}

// startSessionFor creates a session and returns its token; setCookie also
// signs in the requesting browser.
func (s *Server) startSessionFor(w http.ResponseWriter, r *http.Request, u *store.User, device string, setCookie bool) {
	token, err := s.newSession(r, u, device)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if setCookie {
		s.setSessionCookie(w, r, token)
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": u})
}

func (s *Server) newSession(r *http.Request, u *store.User, device string) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	if device == "" {
		device = r.UserAgent()
	}
	return token, s.Store.CreateSession(token, u.ID, device, clientIP(r))
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: 365 * 24 * 3600,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.unlocks.lock(sessionToken(r))
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
	if currentUser(r).OwnerID != 0 {
		writeErr(w, http.StatusForbidden, "profiles sign in through their account and have no password")
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	// Accounts made by single sign-on have no password yet; they can set one.
	if u.HasPassword && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
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
