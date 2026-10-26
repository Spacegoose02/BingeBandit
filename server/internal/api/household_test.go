package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/metadata"
	"bingebandit/internal/avatars"
	"bingebandit/internal/store"
)

func TestProfiles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	owner, _ := st.CreateUser("dana", string(hash), false)
	other, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("dana-token", owner.ID, "Living room TV", "")
	st.CreateSession("sam-token", other.ID, "Web", "")
	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	kidsLib := &store.Library{Name: "Kids", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	st.CreateLibrary(kidsLib)
	h := (&Server{Store: st}).Router(context.Background())

	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}

	var created member
	if c := call("dana-token", "POST", "/api/household", `{"name":"Kids"}`, &created); c != http.StatusCreated {
		t.Fatalf("create: %d", c)
	}
	if c := call("dana-token", "POST", "/api/household", `{"name":"Kids"}`, nil); c != http.StatusConflict {
		t.Fatalf("duplicate name: %d", c)
	}
	// Another account can use the same name.
	if c := call("sam-token", "POST", "/api/household", `{"name":"Kids"}`, nil); c != http.StatusCreated {
		t.Fatalf("same name in another account: %d", c)
	}
	kid := "/api/household/" + itoa(created.ID)
	if c := call("sam-token", "PUT", kid+"/pin", `{"pin":"1234"}`, nil); c != http.StatusNotFound {
		t.Fatalf("someone else's profile: %d", c)
	}
	if c := call("dana-token", "PUT", kid, `{"allLibraries":false,"libraryIds":[`+itoa(kidsLib.ID)+`]}`, nil); c != http.StatusNoContent {
		t.Fatalf("set libraries: %d", c)
	}
	if c := call("dana-token", "PUT", kid+"/pin", `{"pin":"1234"}`, nil); c != http.StatusNoContent {
		t.Fatalf("set pin: %d", c)
	}

	var members []member
	call("dana-token", "GET", "/api/household", "", &members)
	if len(members) != 2 || !members[0].IsOwner || members[1].Name != "Kids" || !members[1].HasPIN || len(members[1].LibIDs) != 1 {
		t.Fatalf("household = %+v", members)
	}

	// Switching needs the PIN, then this device is the profile.
	if c := call("dana-token", "POST", kid+"/switch", `{"pin":"0000"}`, nil); c != http.StatusForbidden {
		t.Fatalf("wrong pin: %d", c)
	}
	var sw struct {
		Token string     `json:"token"`
		User  store.User `json:"user"`
	}
	if c := call("dana-token", "POST", kid+"/switch", `{"pin":"1234"}`, &sw); c != http.StatusOK || sw.User.ProfileName != "Kids" {
		t.Fatalf("switch: %d %+v", c, sw.User)
	}
	if c := call("dana-token", "GET", "/api/me", "", nil); c != http.StatusUnauthorized {
		t.Fatalf("old session after switch: %d", c)
	}
	sessions, _ := st.Sessions(sw.User.ID, sw.Token)
	if len(sessions) != 1 || sessions[0].Device != "Living room TV" {
		t.Fatalf("profile session = %+v", sessions)
	}
	// The profile sees only its library, and can't manage profiles or set a password.
	if !st.CanSeeLibrary(&sw.User, kidsLib.ID) || st.CanSeeLibrary(&sw.User, lib.ID) {
		t.Fatal("profile library access is wrong")
	}
	if c := call(sw.Token, "POST", "/api/household", `{"name":"Teen"}`, nil); c != http.StatusForbidden {
		t.Fatalf("profile creating a profile: %d", c)
	}
	if c := call(sw.Token, "PUT", "/api/me/password", `{"current":"","new":"longenough"}`, nil); c != http.StatusForbidden {
		t.Fatalf("profile setting a password: %d", c)
	}
	// A profile can't sign in directly.
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"Kids (dana)","password":""}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("a profile signed in with no password")
	}

	// Back to the account (no PIN set on it), then delete the profile.
	var back struct {
		Token string `json:"token"`
	}
	if c := call(sw.Token, "POST", "/api/household/"+itoa(owner.ID)+"/switch", `{}`, &back); c != http.StatusOK {
		t.Fatalf("switch back: %d", c)
	}
	if c := call(back.Token, "DELETE", kid, "", nil); c != http.StatusNoContent {
		t.Fatalf("delete: %d", c)
	}
	if _, err := st.UserByID(created.ID); err == nil {
		t.Fatal("profile still exists")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestProfilePicturesAndAutoSelect(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	owner, _ := st.CreateUser("dana", "x", false)
	other, _ := st.CreateUser("sam", "x", false)
	kids, _ := st.CreateProfile(owner, "Kids")
	st.CreateSession("dana-token", owner.ID, "Web", "")
	st.CreateSession("kids-token", kids.ID, "Web", "")
	st.CreateSession("sam-token", other.ID, "Web", "")
	h := (&Server{Store: st, Artwork: metadata.NewArtwork(t.TempDir())}).Router(context.Background())

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")
	upload := func(token string, id int64) int {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", "me.png")
		fw.Write(png)
		mw.Close()
		req := httptest.NewRequest("PUT", "/api/household/"+itoa(id)+"/avatar", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}

	// The account sets anyone's picture, a profile only its own, nobody else's.
	if c := upload("dana-token", kids.ID); c != http.StatusOK {
		t.Fatalf("account sets profile picture: %d", c)
	}
	if c := upload("kids-token", kids.ID); c != http.StatusOK {
		t.Fatalf("profile sets own picture: %d", c)
	}
	if c := upload("kids-token", owner.ID); c != http.StatusNotFound {
		t.Fatalf("profile sets account picture: %d", c)
	}
	if c := upload("sam-token", kids.ID); c != http.StatusNotFound {
		t.Fatalf("other account sets picture: %d", c)
	}

	// Auto-select: only the account, only its own household.
	if c := call("dana-token", "PUT", "/api/household/autoselect", `{"memberId":`+itoa(other.ID)+`}`, nil); c != http.StatusBadRequest {
		t.Fatalf("auto-select someone else: %d", c)
	}
	if c := call("kids-token", "PUT", "/api/household/autoselect", `{"memberId":`+itoa(kids.ID)+`}`, nil); c != http.StatusForbidden {
		t.Fatalf("profile sets auto-select: %d", c)
	}
	if c := call("dana-token", "PUT", "/api/household/autoselect", `{"memberId":`+itoa(kids.ID)+`}`, nil); c != http.StatusNoContent {
		t.Fatalf("auto-select: %d", c)
	}
	var members []member
	call("kids-token", "GET", "/api/household", "", &members)
	if len(members) != 2 || members[0].Auto || !members[1].Auto || !strings.HasPrefix(members[1].Avatar, "/api/images/local/") || !members[1].Custom ||
		!strings.HasPrefix(members[0].Avatar, "/api/avatars/") || members[0].Custom {
		t.Fatalf("household = %+v", members)
	}
	if c := call("kids-token", "DELETE", "/api/household/"+itoa(kids.ID)+"/avatar", "", nil); c != http.StatusOK {
		t.Fatalf("remove picture: %d", c)
	}
	// Back to the built-in picture picked from the id.
	if u, _ := st.UserByID(kids.ID); u.Avatar != avatars.URL(avatars.ForUser(kids.ID)) || u.AvatarCustom {
		t.Fatalf("picture after removing: %q", u.Avatar)
	}

	// Rolling picks another built-in picture, which is served without signing in.
	var rolled map[string]string
	if c := call("kids-token", "POST", "/api/household/"+itoa(kids.ID)+"/avatar/roll", "", &rolled); c != http.StatusOK ||
		rolled["avatar"] == avatars.URL(avatars.ForUser(kids.ID)) || !strings.HasPrefix(rolled["avatar"], "/api/avatars/") {
		t.Fatalf("roll: %d %v", c, rolled)
	}
	if u, _ := st.UserByID(kids.ID); u.Avatar != rolled["avatar"] || u.AvatarCustom {
		t.Fatalf("picture after rolling: %q", u.Avatar)
	}
	if c := call("sam-token", "POST", "/api/household/"+itoa(kids.ID)+"/avatar/roll", "", nil); c != http.StatusNotFound {
		t.Fatalf("other account rolls: %d", c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", rolled["avatar"], nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("serve built-in picture: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/avatars/..%2fapi.go", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("serve unknown picture: %d", rec.Code)
	}
}
