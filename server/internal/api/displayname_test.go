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

func TestDisplayName(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("spacegoose", string(hash), true)
	dana, _ := st.CreateUser("dana", string(hash), false)
	kids, _ := st.CreateProfile(dana, "Kids")
	st.CreateSession("admin-token", admin.ID, "Web", "")
	st.CreateSession("kids-token", kids.ID, "Web", "")
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

	var me store.User
	if c := call("admin-token", "PUT", "/api/me/display-name", `{"name":" Michael "}`, &me); c != http.StatusOK || me.ProfileName != "Michael" || me.Username != "spacegoose" {
		t.Fatalf("set: %d %+v", c, me)
	}
	if u, _ := st.UserByID(admin.ID); memberName(u) != "Michael" || u.Username != "spacegoose" {
		t.Fatalf("stored: %+v", u)
	}
	// Signing in still uses the username.
	if c := call("", "POST", "/api/auth/login", `{"username":"spacegoose","password":"secret-password"}`, nil); c != http.StatusOK {
		t.Fatalf("login by username: %d", c)
	}
	// A profile's name is set by its account, not by itself.
	if c := call("kids-token", "PUT", "/api/me/display-name", `{"name":"Cool kids"}`, nil); c != http.StatusForbidden {
		t.Fatalf("profile renaming itself: %d", c)
	}
	// An admin can name anyone; a profile keeps its stored username in step.
	if c := call("admin-token", "PUT", "/api/admin/users/"+itoa(kids.ID)+"/display-name", `{"name":"Little ones"}`, nil); c != http.StatusNoContent {
		t.Fatalf("admin rename profile: %d", c)
	}
	if u, _ := st.UserByID(kids.ID); u.ProfileName != "Little ones" || u.Username != store.ProfileUsername("Little ones", "dana") {
		t.Fatalf("profile: %+v", u)
	}
	if c := call("admin-token", "PUT", "/api/admin/users/"+itoa(kids.ID)+"/display-name", `{"name":""}`, nil); c != http.StatusBadRequest {
		t.Fatalf("empty profile name: %d", c)
	}
	// Clearing an account's name falls back to the username.
	if c := call("admin-token", "PUT", "/api/admin/users/"+itoa(admin.ID)+"/display-name", `{"name":""}`, nil); c != http.StatusNoContent {
		t.Fatalf("clear: %d", c)
	}
	if u, _ := st.UserByID(admin.ID); memberName(u) != "spacegoose" {
		t.Fatalf("cleared: %+v", u)
	}
}
