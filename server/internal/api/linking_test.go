package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

func TestLinkUsers(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("admin", string(hash), true)
	mom, _ := st.CreateUser("mom", string(hash), false)
	kid, _ := st.CreateUser("timmy", string(hash), false)
	st.CreateSession("admin-token", admin.ID, "Web", "")
	st.CreateSession("mom-token", mom.ID, "Web", "")
	st.CreateSession("kid-token", kid.ID, "Web", "")
	st.SetKid(kid.ID, true)
	pin, _ := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.MinCost)
	st.SetPasscode(kid.ID, string(pin))
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
	link := "/api/admin/users/" + itoa(kid.ID) + "/link"

	if c := call("mom-token", "POST", link, `{"accountId":`+itoa(mom.ID)+`}`, nil); c != http.StatusForbidden {
		t.Fatalf("non-admin linking: %d", c)
	}
	if c := call("admin-token", "POST", "/api/admin/users/"+itoa(admin.ID)+"/link", `{"accountId":`+itoa(mom.ID)+`}`, nil); c != http.StatusBadRequest {
		t.Fatalf("linking an admin: %d", c)
	}
	if c := call("admin-token", "POST", link, `{"accountId":`+itoa(kid.ID)+`}`, nil); c != http.StatusBadRequest {
		t.Fatalf("linking to itself: %d", c)
	}
	if c := call("admin-token", "POST", link, `{"accountId":`+itoa(mom.ID)+`,"name":"Timmy"}`, nil); c != http.StatusNoContent {
		t.Fatalf("link: %d", c)
	}

	// Timmy is now a profile in mom's "Who's watching?", keeping his kid flag
	// and PIN, and can't sign in on his own.
	var members []member
	call("mom-token", "GET", "/api/household", "", &members)
	if len(members) != 2 || members[1].Name != "Timmy" || !members[1].Kid || !members[1].HasPIN {
		t.Fatalf("household = %+v", members)
	}
	if c := call("kid-token", "GET", "/api/household", "", nil); c != http.StatusUnauthorized {
		t.Fatalf("old session still works: %d", c)
	}
	u, _ := st.UserByID(kid.ID)
	if u.HasPassword || u.OwnerID != mom.ID {
		t.Fatalf("linked user = %+v", u)
	}
	if c := call("admin-token", "POST", link, `{"accountId":`+itoa(mom.ID)+`}`, nil); c != http.StatusBadRequest {
		t.Fatalf("linking a profile again: %d", c)
	}
	// A profile can't take others under it.
	other, _ := st.CreateUser("sam", string(hash), false)
	if c := call("admin-token", "POST", "/api/admin/users/"+itoa(other.ID)+"/link", `{"accountId":`+itoa(kid.ID)+`}`, nil); c != http.StatusBadRequest {
		t.Fatalf("linking under a profile: %d", c)
	}

	// An account with profiles brings them along.
	st.CreateSession("sam-token", other.ID, "Web", "")
	if c := call("sam-token", "POST", "/api/household", `{"name":"Guest"}`, nil); c != http.StatusCreated {
		t.Fatalf("create profile: %d", c)
	}
	if c := call("admin-token", "POST", "/api/admin/users/"+itoa(other.ID)+"/link", `{"accountId":`+itoa(mom.ID)+`}`, nil); c != http.StatusNoContent {
		t.Fatalf("link with profiles: %d", c)
	}
	call("mom-token", "GET", "/api/household", "", &members)
	if len(members) != 4 || members[2].Name != "sam" || members[3].Name != "Guest" {
		t.Fatalf("household with sam = %+v", members)
	}

	// Unlinking makes Timmy his own account again.
	unlink := "/api/admin/users/" + itoa(kid.ID) + "/unlink"
	if c := call("admin-token", "POST", unlink, `{"username":"mom","password":"another-password"}`, nil); c != http.StatusConflict {
		t.Fatalf("taken username: %d", c)
	}
	if c := call("admin-token", "POST", unlink, `{"username":"timmy","password":"another-password"}`, nil); c != http.StatusNoContent {
		t.Fatalf("unlink: %d", c)
	}
	u, _ = st.UserByID(kid.ID)
	if u.OwnerID != 0 || u.Username != "timmy" || u.ProfileName != "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("another-password")) != nil || !u.Kid {
		t.Fatalf("unlinked user = %+v", u)
	}
	call("mom-token", "GET", "/api/household", "", &members)
	if len(members) != 3 {
		t.Fatalf("household after unlink = %+v", members)
	}
	if c := call("admin-token", "POST", "/api/admin/users/"+itoa(mom.ID)+"/unlink", `{"username":"mommy","password":"another-password"}`, nil); c != http.StatusBadRequest {
		t.Fatalf("unlinking an account: %d", c)
	}

	// Emails for single sign-on: one user each, any letter case.
	email := "/api/admin/users/" + itoa(kid.ID) + "/email"
	if c := call("admin-token", "PUT", email, `{"email":"not an email"}`, nil); c != http.StatusBadRequest {
		t.Fatalf("bad email: %d", c)
	}
	if c := call("admin-token", "PUT", email, `{"email":"Timmy@Example.com"}`, nil); c != http.StatusNoContent {
		t.Fatalf("set email: %d", c)
	}
	if c := call("admin-token", "PUT", "/api/admin/users/"+itoa(mom.ID)+"/email", `{"email":"timmy@example.com"}`, nil); c != http.StatusConflict {
		t.Fatalf("same email twice: %d", c)
	}
	if u, err := st.UserByEmail("timmy@EXAMPLE.com"); err != nil || u.ID != kid.ID {
		t.Fatalf("by email = %+v, %v", u, err)
	}
	if c := call("admin-token", "PUT", email, `{"email":""}`, nil); c != http.StatusNoContent {
		t.Fatalf("clear email: %d", c)
	}
	if _, err := st.UserByEmail("timmy@example.com"); err == nil {
		t.Fatal("email still attached")
	}
}

func TestEmailVerified(t *testing.T) {
	for v, want := range map[any]bool{true: true, false: false, "true": true, "TRUE": true, "false": false, nil: false} {
		if got := emailVerified(v); got != want {
			t.Errorf("emailVerified(%v) = %v", v, got)
		}
	}
}
