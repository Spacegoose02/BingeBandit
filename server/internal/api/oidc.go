package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"bingebandit/internal/store"
)

// Single sign-on with OpenID Connect providers (Google, TinyAuth, Authelia,
// Authentik, Keycloak…). The browser goes to the provider and comes back to
// /api/auth/oidc/callback; the pending sign-in lives in memory, keyed by the
// random state, for ten minutes.

const oidcTTL = 10 * time.Minute

type oidcPending struct {
	providerID int64
	verifier   string // PKCE
	nonce      string
	userID     int64 // non-zero when linking to a signed-in user
	next       string
	expires    time.Time
}

type discovered struct {
	p       *oidc.Provider
	fetched time.Time
}

type oidcFlows struct {
	mu        sync.Mutex
	pending   map[string]*oidcPending
	providers map[string]discovered // by issuer
}

func newOIDCFlows() *oidcFlows {
	return &oidcFlows{pending: map[string]*oidcPending{}, providers: map[string]discovered{}}
}

var oidcHTTP = &http.Client{Timeout: 15 * time.Second}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// discover fetches (and caches for an hour) the provider's discovery document.
func (f *oidcFlows) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	f.mu.Lock()
	d, ok := f.providers[issuer]
	f.mu.Unlock()
	if ok && time.Since(d.fetched) < time.Hour {
		return d.p, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, oidcHTTP), issuer)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.providers[issuer] = discovered{p, time.Now()}
	f.mu.Unlock()
	return p, nil
}

func (f *oidcFlows) put(p *oidcPending) string {
	state := randHex(24)
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for k, v := range f.pending {
		if now.After(v.expires) {
			delete(f.pending, k)
		}
	}
	p.expires = now.Add(oidcTTL)
	f.pending[state] = p
	return state
}

func (f *oidcFlows) take(state string) *oidcPending {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pending[state]
	delete(f.pending, state)
	if p == nil || time.Now().After(p.expires) {
		return nil
	}
	return p
}

func (s *Server) oidcRedirectURL(r *http.Request) string {
	return s.externalURL(r) + "/api/auth/oidc/callback"
}

func (s *Server) oauthConfig(r *http.Request, prov *store.OIDCProvider, op *oidc.Provider) *oauth2.Config {
	scopes := []string{oidc.ScopeOpenID, "profile", "email"}
	for _, sc := range strings.Fields(strings.ReplaceAll(prov.Scopes, ",", " ")) {
		if !contains(scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	return &oauth2.Config{
		ClientID: prov.ClientID, ClientSecret: prov.ClientSecret, Endpoint: op.Endpoint(),
		RedirectURL: s.oidcRedirectURL(r), Scopes: scopes,
	}
}

// authURL starts a sign-in (userID 0) or a link (userID set) and returns where to send the browser.
func (s *Server) authURL(r *http.Request, providerID, userID int64, next string) (string, error) {
	prov, err := s.Store.OIDCProvider(providerID)
	if err != nil || !prov.Enabled {
		return "", errors.New("that sign-in option isn't available")
	}
	op, err := s.oidc.discover(r.Context(), prov.Issuer)
	if err != nil {
		log.Printf("oidc: %s discovery: %v", prov.Name, err)
		return "", fmt.Errorf("couldn't reach %s; try again later", prov.Name)
	}
	pend := &oidcPending{providerID: prov.ID, verifier: oauth2.GenerateVerifier(), nonce: randHex(16), userID: userID, next: next}
	state := s.oidc.put(pend)
	return s.oauthConfig(r, prov, op).AuthCodeURL(state, oidc.Nonce(pend.nonce), oauth2.S256ChallengeOption(pend.verifier)), nil
}

// safeNext keeps post-login redirects on this site.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.HasPrefix(next, "/api/") {
		return "/"
	}
	return next
}

// oidcOptions lists the enabled providers for the login page (no auth).
func (s *Server) oidcOptions(w http.ResponseWriter, r *http.Request) {
	provs, err := s.Store.OIDCProviders()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type opt struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	out := []opt{}
	for _, p := range provs {
		if p.Enabled {
			out = append(out, opt{p.ID, p.Name})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// oidcLogin redirects the browser to the provider.
func (s *Server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, err := s.authURL(r, id, 0, safeNext(r.URL.Query().Get("next")))
	if err != nil {
		http.Redirect(w, r, "/login?sso_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// linkIdentity returns the provider URL that links an account to the signed-in user.
func (s *Server) linkIdentity(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u, err := s.authURL(r, id, currentUser(r).ID, "/account")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": u})
}

type oidcClaims struct {
	Email             string `json:"email"`
	EmailVerified     any    `json:"email_verified"` // some providers send "true" as a string
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

// emailVerified reads the email_verified claim, which some providers send as
// a string.
func emailVerified(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

// oidcCallback finishes a sign-in or link.
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pend := s.oidc.take(q.Get("state"))
	fail := func(dest, msg string) {
		http.Redirect(w, r, dest+"?sso_error="+url.QueryEscape(msg), http.StatusFound)
	}
	if pend == nil {
		fail("/login", "That sign-in took too long or was already used. Try again.")
		return
	}
	errDest := "/login"
	if pend.userID != 0 {
		errDest = "/account"
	}
	if e := q.Get("error"); e != "" {
		msg := q.Get("error_description")
		if msg == "" {
			msg = e
		}
		fail(errDest, "The provider said: "+msg)
		return
	}
	prov, err := s.Store.OIDCProvider(pend.providerID)
	if err != nil {
		fail(errDest, "That sign-in option was removed.")
		return
	}
	ctx := oidc.ClientContext(r.Context(), oidcHTTP)
	op, err := s.oidc.discover(ctx, prov.Issuer)
	if err != nil {
		fail(errDest, "Couldn't reach "+prov.Name+".")
		return
	}
	tok, err := s.oauthConfig(r, prov, op).Exchange(ctx, q.Get("code"), oauth2.VerifierOption(pend.verifier))
	if err != nil {
		log.Printf("oidc: %s token exchange: %v", prov.Name, err)
		fail(errDest, prov.Name+" didn't accept the sign-in. Check the client ID and secret.")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := op.Verifier(&oidc.Config{ClientID: prov.ClientID}).Verify(ctx, raw)
	if err != nil || idt.Nonce != pend.nonce {
		log.Printf("oidc: %s id token: %v", prov.Name, err)
		fail(errDest, prov.Name+" sent a sign-in that couldn't be verified.")
		return
	}
	var c oidcClaims
	idt.Claims(&c)

	if pend.userID != 0 {
		switch err := s.Store.LinkIdentity(pend.userID, prov.ID, idt.Subject, c.Email); {
		case errors.Is(err, store.ErrIdentityTaken):
			fail("/account", "That "+prov.Name+" account is already linked to another user.")
		case err != nil:
			writeStoreErr(w, err)
		default:
			http.Redirect(w, r, "/account?linked="+url.QueryEscape(prov.Name), http.StatusFound)
		}
		return
	}

	u, err := s.Store.UserByIdentity(prov.ID, idt.Subject, c.Email)
	if errors.Is(err, store.ErrNotFound) && c.Email != "" {
		// An admin attached this email to a user: link it on first use.
		if byEmail, e := s.Store.UserByEmail(c.Email); e == nil {
			if !emailVerified(c.EmailVerified) {
				fail("/login", prov.Name+" hasn't confirmed "+c.Email+", so it can't be used to sign in here yet.")
				return
			}
			if e := s.Store.LinkIdentity(byEmail.ID, prov.ID, idt.Subject, c.Email); e != nil {
				writeStoreErr(w, e)
				return
			}
			log.Printf("oidc: linked a %s sign-in to %q by email", prov.Name, byEmail.Username)
			u, err = byEmail, nil
		}
	}
	if errors.Is(err, store.ErrNotFound) && prov.AutoCreate {
		u, err = s.createSSOUser(prov, idt.Subject, c)
	}
	if errors.Is(err, store.ErrNotFound) {
		fail("/login", "No account here is linked to that "+prov.Name+" account yet. Sign in with your password, then link it under Account.")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	// The provider did the authenticating (including its own 2FA), so
	// BingeBandit's TOTP isn't asked for again here.
	if err := s.Store.CreateSession(randSessionToken(w, r), u.ID, prov.Name+" sign-in", clientIP(r)); err != nil {
		writeStoreErr(w, err)
		return
	}
	http.Redirect(w, r, safeNext(pend.next), http.StatusFound)
}

var reUsernameJunk = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// createSSOUser makes a passwordless account named after the provider profile.
func (s *Server) createSSOUser(prov *store.OIDCProvider, subject string, c oidcClaims) (*store.User, error) {
	base := c.PreferredUsername
	if base == "" {
		base, _, _ = strings.Cut(c.Email, "@")
	}
	if base == "" {
		base = c.Name
	}
	name := s.uniqueUsername(base)
	u, err := s.Store.CreateUser(name, "", false)
	if err != nil {
		return nil, err
	}
	if err := s.Store.LinkIdentity(u.ID, prov.ID, subject, c.Email); err != nil {
		return nil, err
	}
	log.Printf("oidc: created user %q from a %s sign-in", name, prov.Name)
	return u, nil
}

// randSessionToken makes a session token and sets it as the browser cookie.
func randSessionToken(w http.ResponseWriter, r *http.Request) string {
	token := randHex(32)
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: 365 * 24 * 3600,
	})
	return token
}

// ---- the user's linked accounts ----

func (s *Server) identities(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Identities(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) unlinkIdentity(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u := currentUser(r)
	if !u.HasPassword {
		if list, err := s.Store.Identities(u.ID); err == nil && len(list) <= 1 {
			writeErr(w, http.StatusConflict, "set a password first, or you won't be able to sign in")
			return
		}
	}
	if err := s.Store.UnlinkIdentity(u.ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- admin ----

func (s *Server) adminOIDC(w http.ResponseWriter, r *http.Request) {
	provs, err := s.Store.OIDCProviders()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, p := range provs {
		p.ClientSecret = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": provs, "redirectUrl": s.oidcRedirectURL(r)})
}

func (s *Server) saveOIDC(w http.ResponseWriter, r *http.Request) {
	var p store.OIDCProvider
	if err := readJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if id, ok := idParam(r, "id"); ok {
		p.ID = id
	} else if chi.URLParam(r, "id") != "" {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	p.Name, p.Issuer, p.ClientID = strings.TrimSpace(p.Name), strings.TrimRight(strings.TrimSpace(p.Issuer), "/"), strings.TrimSpace(p.ClientID)
	p.ClientSecret, p.Scopes = strings.TrimSpace(p.ClientSecret), strings.TrimSpace(p.Scopes)
	iu, err := url.Parse(p.Issuer)
	if p.Name == "" || p.ClientID == "" || err != nil || (iu.Scheme != "https" && iu.Scheme != "http") || iu.Host == "" {
		writeErr(w, http.StatusBadRequest, "a provider needs a name, an issuer URL and a client ID")
		return
	}
	// Check the issuer now so typos show up here, not at someone's sign-in.
	if _, err := s.oidc.discover(r.Context(), p.Issuer); err != nil {
		writeErr(w, http.StatusBadRequest, "couldn't load "+p.Issuer+"/.well-known/openid-configuration: "+err.Error())
		return
	}
	if err := s.Store.SaveOIDCProvider(&p); err != nil {
		writeStoreErr(w, err)
		return
	}
	saved, err := s.Store.OIDCProvider(p.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	saved.ClientSecret = ""
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteOIDC(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteOIDCProvider(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
