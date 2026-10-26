package api

import (
	"errors"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/avatars"
	"bingebandit/internal/store"
)

// Profiles: one sign-in, several people. Each profile is its own user under
// the account (owner_id), so watch history, Continue Watching, the watchlist,
// playlists and preferences stay apart with no changes elsewhere. A profile
// has no password: you reach it by switching from another member of the
// household, entering its PIN if it has one. A profile's PIN is its settings
// passcode, and the account's PIN is the account's settings passcode.

const maxProfiles = 10

type member struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	IsOwner bool    `json:"isOwner"`
	HasPIN  bool    `json:"hasPin"`
	Current bool    `json:"current"`
	Avatar  string  `json:"avatar,omitempty"`
	Custom  bool    `json:"avatarCustom,omitempty"` // an uploaded picture, not a built-in one
	Auto    bool    `json:"auto,omitempty"`         // the account opens as this member without asking
	Kid     bool    `json:"kid,omitempty"`          // only sees Live TV channels approved for kids
	AllLibs bool    `json:"allLibraries"`
	LibIDs  []int64 `json:"libraryIds,omitempty"`
}

func memberName(u *store.User) string {
	if u.ProfileName != "" {
		return u.ProfileName
	}
	return u.Username
}

func (s *Server) household(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	users, err := s.Store.Household(u.HouseholdID())
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var auto int64
	for _, m := range users {
		if m.OwnerID == 0 {
			auto = m.AutoProfileID
		}
	}
	out := make([]member, 0, len(users))
	for _, m := range users {
		mem := member{ID: m.ID, Name: memberName(m), IsOwner: m.OwnerID == 0, HasPIN: m.HasPasscode, Current: m.ID == u.ID,
			Avatar: m.Avatar, Custom: m.AvatarCustom, Auto: m.ID == auto, Kid: m.Kid, AllLibs: m.IsAdmin || m.AllLibraries}
		// Only the account sees what each profile may browse.
		if u.OwnerID == 0 && m.OwnerID != 0 && !m.AllLibraries {
			mem.LibIDs, _ = s.Store.UserLibraryIDs(m.ID)
		}
		out = append(out, mem)
	}
	writeJSON(w, http.StatusOK, out)
}

func validProfileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 30 || strings.ContainsAny(name, "()") {
		return "", errors.New("use a name of up to 30 characters, without brackets")
	}
	return name, nil
}

// accountOnly returns the signed-in account, refusing profiles: only the
// account manages its profiles.
func accountOnly(w http.ResponseWriter, r *http.Request) *store.User {
	u := currentUser(r)
	if u.OwnerID != 0 {
		writeErr(w, http.StatusForbidden, "switch to the account to manage profiles")
		return nil
	}
	return u
}

// ownProfile loads profile {id} if it belongs to the signed-in account.
func (s *Server) ownProfile(w http.ResponseWriter, r *http.Request) (*store.User, *store.User) {
	owner := accountOnly(w, r)
	if owner == nil {
		return nil, nil
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil, nil
	}
	p, err := s.Store.UserByID(id)
	if err != nil || p.OwnerID != owner.ID {
		writeErr(w, http.StatusNotFound, "not found")
		return nil, nil
	}
	return owner, p
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	owner := accountOnly(w, r)
	if owner == nil {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	name, err := validProfileName(req.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	members, err := s.Store.Household(owner.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if len(members)-1 >= maxProfiles {
		writeErr(w, http.StatusBadRequest, "an account can have up to "+strconv.Itoa(maxProfiles)+" profiles")
		return
	}
	p, err := s.Store.CreateProfile(owner, name)
	if errors.Is(err, store.ErrExists) {
		writeErr(w, http.StatusConflict, "you already have a profile with that name")
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, member{ID: p.ID, Name: p.ProfileName, AllLibs: p.AllLibraries})
}

// updateProfile renames a profile, sets which libraries it sees (never more
// than the account itself sees) and whether it's a kid.
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	owner, p := s.ownProfile(w, r)
	if p == nil {
		return
	}
	var req struct {
		Name         *string `json:"name"`
		AllLibraries *bool   `json:"allLibraries"`
		LibraryIDs   []int64 `json:"libraryIds"`
		Kid          *bool   `json:"kid"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name != nil {
		name, err := validProfileName(*req.Name)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.Store.RenameProfile(p.ID, name, owner.Username); errors.Is(err, store.ErrExists) {
			writeErr(w, http.StatusConflict, "you already have a profile with that name")
			return
		} else if err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	if req.AllLibraries != nil {
		all := *req.AllLibraries
		ids := []int64{}
		if !owner.IsAdmin && !owner.AllLibraries {
			all = false
		}
		if !all {
			for _, id := range req.LibraryIDs {
				if s.Store.CanSeeLibrary(owner, id) {
					ids = append(ids, id)
				}
			}
		}
		if err := s.Store.SetUserLibraries(p.ID, all, ids); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	if req.Kid != nil {
		if err := s.Store.SetKid(p.ID, *req.Kid); err != nil {
			writeStoreErr(w, err)
			return
		}
		if *req.Kid {
			s.Live.KickUser(p.ID)
		}
	}
	s.recheckTranscodes()
	w.WriteHeader(http.StatusNoContent)
}

// setProfilePIN sets or (with an empty pin) removes a profile's PIN.
func (s *Server) setProfilePIN(w http.ResponseWriter, r *http.Request) {
	_, p := s.ownProfile(w, r)
	if p == nil {
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	hash := ""
	if req.PIN != "" {
		if !passcodeFormat.MatchString(req.PIN) {
			writeErr(w, http.StatusBadRequest, "use 4 to 8 digits")
			return
		}
		b, err := bcrypt.GenerateFromPassword([]byte(req.PIN), bcrypt.DefaultCost)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		hash = string(b)
	}
	if err := s.Store.SetPasscode(p.ID, hash); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	_, p := s.ownProfile(w, r)
	if p == nil {
		return
	}
	if err := s.Store.DeleteUser(p.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.recheckTranscodes()
	w.WriteHeader(http.StatusNoContent)
}

// householdMember loads member {id} if the signed-in user may change its
// picture: the account for anyone in its household, a profile for itself.
func (s *Server) householdMember(w http.ResponseWriter, r *http.Request) *store.User {
	u := currentUser(r)
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	m, err := s.Store.UserByID(id)
	if err != nil || (m.ID != u.ID && (u.OwnerID != 0 || m.OwnerID != u.ID)) {
		writeErr(w, http.StatusNotFound, "not found")
		return nil
	}
	return m
}

// maxAvatarBytes caps a profile picture upload. The web app crops and shrinks
// pictures before sending them, so real ones are far smaller.
const maxAvatarBytes = 5 << 20

// setAvatar stores an uploaded profile picture (the "file" field).
func (s *Server) setAvatar(w http.ResponseWriter, r *http.Request) {
	m := s.householdMember(w, r)
	if m == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+64<<10)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "attach a JPEG, PNG or WebP picture of up to 5 MB as the \"file\" field")
		return
	}
	defer file.Close()
	ref, err := s.Artwork.Save(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.SetAvatar(m.ID, ref); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar": store.ImageURL(ref)})
}

// rollAvatar swaps a member's picture for another built-in one at random.
func (s *Server) rollAvatar(w http.ResponseWriter, r *http.Request) {
	m := s.householdMember(w, r)
	if m == nil {
		return
	}
	current := strings.TrimSuffix(path.Base(m.Avatar), ".webp")
	ref := avatars.RefPrefix + avatars.Roll(current)
	if err := s.Store.SetAvatar(m.ID, ref); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar": store.ImageURL(ref)})
}

func (s *Server) removeAvatar(w http.ResponseWriter, r *http.Request) {
	m := s.householdMember(w, r)
	if m == nil {
		return
	}
	if err := s.Store.SetAvatar(m.ID, ""); err != nil {
		writeStoreErr(w, err)
		return
	}
	// The built-in picture the member has again.
	writeJSON(w, http.StatusOK, map[string]string{"avatar": avatars.URL(avatars.ForUser(m.ID))})
}

// setAutoProfile picks the member this account opens as on every device
// without asking who's watching; 0 asks every time. A member with a PIN still
// asks for it.
func (s *Server) setAutoProfile(w http.ResponseWriter, r *http.Request) {
	owner := accountOnly(w, r)
	if owner == nil {
		return
	}
	var req struct {
		MemberID int64 `json:"memberId"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.MemberID != 0 {
		m, err := s.Store.UserByID(req.MemberID)
		if err != nil || m.HouseholdID() != owner.ID {
			writeErr(w, http.StatusBadRequest, "pick yourself or one of your profiles")
			return
		}
	}
	if err := s.Store.SetAutoProfile(owner.ID, req.MemberID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// switchProfile signs this device in as another member of the household. A
// member with a PIN needs it. The old session ends, so Back can't return to a
// grown-up's profile without the PIN.
func (s *Server) switchProfile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	token := sessionToken(r)
	if token == "" {
		writeErr(w, http.StatusBadRequest, "API keys can't switch profiles")
		return
	}
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	target, err := s.Store.UserByID(id)
	if err != nil || target.HouseholdID() != u.HouseholdID() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if target.HasPasscode && target.ID != u.ID {
		key := "pin:" + strconv.FormatInt(target.ID, 10) + ":" + clientIP(r)
		if !s.limiter.allow(key) {
			writeErr(w, http.StatusTooManyRequests, "too many wrong tries; wait 15 minutes")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(target.PasscodeHash), []byte(req.PIN)) != nil {
			s.limiter.fail(key)
			// Not 401: that would sign the web UI out.
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Wrong PIN", "needsPin": true})
			return
		}
	}
	device := ""
	if sessions, err := s.Store.Sessions(u.ID, token); err == nil {
		for _, sess := range sessions {
			if sess.Current {
				device = sess.Device
			}
		}
	}
	newToken, err := s.newSession(r, target, device)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.unlocks.lock(token)
	s.Store.DeleteSession(token)
	// Entering the PIN proves who this is, so their settings start unlocked.
	if target.HasPasscode {
		s.unlocks.extend(newToken)
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		s.setSessionCookie(w, r, newToken)
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": newToken, "user": target})
}

// linkUser (admin) moves an account under another account as a profile, so
// they share one sign-in and it shows in that account's "Who's watching?".
// Its watch history and settings stay; its own password, two-step sign-in
// and passkeys go, and it's signed out everywhere. Single sign-on and an
// attached email keep working and land in the profile. Profiles
// it already had come along. Admins can't be linked, so switching profiles
// never leads to admin rights.
func (s *Server) linkUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		AccountID int64  `json:"accountId"`
		Name      string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, err := s.Store.UserByID(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	switch {
	case u.OwnerID != 0:
		writeErr(w, http.StatusBadRequest, "that's already a profile; unlink it first")
		return
	case u.IsAdmin:
		writeErr(w, http.StatusBadRequest, "admins can't become profiles")
		return
	case u.ID == req.AccountID:
		writeErr(w, http.StatusBadRequest, "pick another account")
		return
	}
	owner, err := s.Store.UserByID(req.AccountID)
	if err != nil || owner.OwnerID != 0 {
		writeErr(w, http.StatusBadRequest, "pick an account, not a profile")
		return
	}
	if req.Name == "" {
		req.Name = u.Username
	}
	name, err := validProfileName(req.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	theirs, err := s.Store.Household(u.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	members, err := s.Store.Household(owner.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if len(members)-1+len(theirs) > maxProfiles {
		writeErr(w, http.StatusBadRequest, "an account can have up to "+strconv.Itoa(maxProfiles)+" profiles")
		return
	}
	if err := s.Store.LinkUser(u, owner, name); errors.Is(err, store.ErrExists) {
		writeErr(w, http.StatusConflict, owner.Username+" already has a profile with that name")
		return
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unlinkUser (admin) makes a profile an account of its own again, with the
// username and password given. Everything else it had stays.
func (s *Server) unlinkUser(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req credentials
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := validCredentials(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := s.Store.UserByID(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if p.OwnerID == 0 {
		writeErr(w, http.StatusBadRequest, "that's not a profile")
		return
	}
	username := strings.TrimSpace(req.Username)
	if other, err := s.Store.UserByUsername(username); err == nil && other.ID != p.ID {
		writeErr(w, http.StatusConflict, "that username is taken")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err := s.Store.UnlinkProfile(p, username, string(hash)); errors.Is(err, store.ErrExists) {
		writeErr(w, http.StatusConflict, "that username is taken")
		return
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var emailFormat = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// setUserEmail (admin) attaches an email to a user: signing in with single
// sign-on as that email then lands in this user, profile or account.
func (s *Server) setUserEmail(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email != "" && !emailFormat.MatchString(email) {
		writeErr(w, http.StatusBadRequest, "that doesn't look like an email address")
		return
	}
	if err := s.Store.SetEmail(id, email); errors.Is(err, store.ErrExists) {
		writeErr(w, http.StatusConflict, "another user already has that email")
		return
	} else if err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
