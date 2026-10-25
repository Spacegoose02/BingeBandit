package api

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"bingebandit/internal/store"
)

// Passkeys (WebAuthn) sign an account in without a password or 2FA code:
// the device already checks something you have and something you are or
// know. Browsers only offer them on https:// pages (or localhost), for the
// address the server was registered on.

const passkeyFlowTTL = 5 * time.Minute

// passkeyFlows keeps each ceremony's challenge on the server between the
// begin and finish calls.
type passkeyFlows struct {
	mu      sync.Mutex
	pending map[string]passkeyFlow
}

type passkeyFlow struct {
	session webauthn.SessionData
	rpID    string
	userID  int64 // registration only
	expires time.Time
}

func (f *passkeyFlows) put(flow passkeyFlow) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = map[string]passkeyFlow{}
	}
	now := time.Now()
	for id, p := range f.pending {
		if now.After(p.expires) {
			delete(f.pending, id)
		}
	}
	id := randHex(16)
	flow.expires = now.Add(passkeyFlowTTL)
	f.pending[id] = flow
	return id
}

// take returns a flow once; a second finish with the same id fails.
func (f *passkeyFlows) take(id string) (passkeyFlow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pending[id]
	delete(f.pending, id)
	return p, ok && time.Now().Before(p.expires)
}

// passkeyUser adapts a user and their passkeys to the WebAuthn library.
type passkeyUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func userHandle(id int64) []byte { return binary.BigEndian.AppendUint64(nil, uint64(id)) }

func (p *passkeyUser) WebAuthnID() []byte                         { return userHandle(p.u.ID) }
func (p *passkeyUser) WebAuthnName() string                       { return p.u.Username }
func (p *passkeyUser) WebAuthnDisplayName() string                { return p.u.Username }
func (p *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return p.creds }

func (s *Server) loadPasskeyUser(u *store.User) (*passkeyUser, error) {
	keys, err := s.Store.Passkeys(u.ID)
	if err != nil {
		return nil, err
	}
	pu := &passkeyUser{u: u}
	for _, k := range keys {
		var c webauthn.Credential
		if json.Unmarshal([]byte(k.Credential), &c) == nil {
			pu.creds = append(pu.creds, c)
		}
	}
	return pu, nil
}

// webAuthn is configured per request for the address the browser is on: the
// page's Origin when it is this server's own address (the Host it was
// reached on, or the public URL), else the address of the request.
func (s *Server) webAuthn(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := baseURL(r)
	if o := r.Header.Get("Origin"); o != "" {
		if ou, err := url.Parse(o); err == nil && ou.Host != "" {
			allowed := strings.EqualFold(ou.Host, r.Host)
			if pu, err := url.Parse(s.policy().publicURL); err == nil && pu.Host != "" && strings.EqualFold(ou.Host, pu.Host) {
				allowed = true
			}
			if allowed {
				origin = ou.Scheme + "://" + ou.Host
			}
		}
	}
	u, err := url.Parse(origin)
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: s.Store.Setting(store.SettingServerName),
		RPOrigins:     []string{origin},
	})
}

// passkeyError explains a failed ceremony; the library's own errors carry
// the useful part in DevInfo.
func passkeyError(err error) string {
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.DevInfo != "" {
		return pe.Details + ": " + pe.DevInfo
	}
	return err.Error()
}

// ---- signing in ----

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "passkeys don't work on this address: "+err.Error())
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, passkeyError(err))
		return
	}
	id := s.passkeys.put(passkeyFlow{session: *session, rpID: wa.Config.RPID})
	writeJSON(w, http.StatusOK, map[string]any{"flow": id, "options": assertion.Response})
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	var req struct {
		Flow       string          `json:"flow"`
		Device     string          `json:"device"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	flow, ok := s.passkeys.take(req.Flow)
	if !ok {
		writeErr(w, http.StatusBadRequest, "the sign-in took too long; try again")
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil || wa.Config.RPID != flow.rpID {
		writeErr(w, http.StatusBadRequest, "the sign-in started on another address; try again")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(req.Credential)
	if err != nil {
		s.limiter.fail(ip)
		writeErr(w, http.StatusBadRequest, "the passkey's answer couldn't be read: "+passkeyError(err))
		return
	}
	var key *store.Passkey
	var user *store.User
	var lookupErr error
	handler := func(rawID, handle []byte) (webauthn.User, error) {
		k, err := s.Store.PasskeyByCredentialID(rawID)
		if err != nil {
			lookupErr = errors.New("this passkey isn't registered on this server (it may have been removed)")
			return nil, lookupErr
		}
		if len(handle) != 8 || int64(binary.BigEndian.Uint64(handle)) != k.UserID {
			lookupErr = errors.New("this passkey belongs to another account")
			return nil, lookupErr
		}
		u, err := s.Store.UserByID(k.UserID)
		if err != nil {
			lookupErr = errors.New("this passkey's account no longer exists")
			return nil, lookupErr
		}
		key, user = k, u
		return s.loadPasskeyUser(u)
	}
	cred, err := wa.ValidateDiscoverableLogin(handler, flow.session, parsed)
	if err != nil {
		s.limiter.fail(ip)
		if lookupErr != nil {
			writeErr(w, http.StatusUnauthorized, "Passkey sign-in failed: "+lookupErr.Error())
		} else {
			writeErr(w, http.StatusUnauthorized, "Passkey sign-in failed: "+passkeyError(err))
		}
		return
	}
	if cred.Authenticator.CloneWarning {
		log.Printf("auth: passkey %q of %s reported a sign count that went backwards (a cloned key?); signing in anyway", key.Name, user.Username)
	}
	if b, err := json.Marshal(cred); err == nil {
		s.Store.UsePasskey(key.ID, string(b))
	}
	s.startSession(w, r, user, req.Device)
}

// ---- managing your passkeys ----

func (s *Server) myPasskeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Store.Passkeys(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.OwnerID != 0 {
		writeErr(w, http.StatusForbidden, "profiles sign in through their account")
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "passkeys don't work on this address: "+err.Error())
		return
	}
	pu, err := s.loadPasskeyUser(u)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	creation, session, err := wa.BeginRegistration(pu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(pu.creds).CredentialDescriptors()),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, passkeyError(err))
		return
	}
	id := s.passkeys.put(passkeyFlow{session: *session, rpID: wa.Config.RPID, userID: u.ID})
	writeJSON(w, http.StatusOK, map[string]any{"flow": id, "options": creation.Response})
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		Flow       string          `json:"flow"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	flow, ok := s.passkeys.take(req.Flow)
	if !ok || flow.userID != u.ID {
		writeErr(w, http.StatusBadRequest, "adding the passkey took too long; try again")
		return
	}
	wa, err := s.webAuthn(r)
	if err != nil || wa.Config.RPID != flow.rpID {
		writeErr(w, http.StatusBadRequest, "adding the passkey started on another address; try again")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the passkey's answer couldn't be read: "+passkeyError(err))
		return
	}
	pu, err := s.loadPasskeyUser(u)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	cred, err := wa.CreateCredential(pu, flow.session, parsed)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the passkey wasn't accepted: "+passkeyError(err))
		return
	}
	b, _ := json.Marshal(cred)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("Passkey added %s", time.Now().Format("Jan 2, 2006"))
	}
	if len(name) > 60 {
		name = name[:60]
	}
	key := &store.Passkey{UserID: u.ID, CredentialID: cred.ID, Credential: string(b), Name: name}
	if err := s.Store.AddPasskey(key); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (s *Server) renamePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" || len(req.Name) > 60 {
		writeErr(w, http.StatusBadRequest, "give the passkey a name of up to 60 characters")
		return
	}
	if err := s.Store.RenamePasskey(currentUser(r).ID, id, strings.TrimSpace(req.Name)); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeletePasskey(currentUser(r).ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminDeletePasskeys removes every passkey of a user (a lost phone, a
// passkey someone else may have).
func (s *Server) adminDeletePasskeys(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeletePasskeys(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
