package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

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
