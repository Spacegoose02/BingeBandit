package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

func TestSettingsPasscode(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	u, err := st.CreateUser("admin", string(hash), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("web-token", u.ID, "Web", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("other-token", u.ID, "TV", ""); err != nil {
		t.Fatal(err)
	}
	key, _, err := st.CreateAPIKey(u.ID, "script")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st}
	h := s.Router(context.Background())

	call := func(token, method, path, body string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// No passcode yet: nothing is locked.
	if c := call("web-token", "GET", "/api/admin/settings", ""); c != http.StatusOK {
		t.Fatalf("admin settings without a passcode: %d", c)
	}
	if c := call("web-token", "PUT", "/api/me/passcode", `{"passcode":"12"}`); c != http.StatusBadRequest {
		t.Fatalf("short passcode: %d", c)
	}
	if c := call("web-token", "PUT", "/api/me/passcode", `{"passcode":"2468"}`); c != http.StatusNoContent {
		t.Fatalf("set passcode: %d", c)
	}

	// The session that set it stays unlocked; another one is locked.
	if c := call("web-token", "GET", "/api/admin/settings", ""); c != http.StatusOK {
		t.Fatalf("setting session after set: %d", c)
	}
	if c := call("other-token", "GET", "/api/admin/settings", ""); c != http.StatusLocked {
		t.Fatalf("other session: %d, want 423", c)
	}
	if c := call("other-token", "PUT", "/api/me/prefs", `{}`); c != http.StatusLocked {
		t.Fatalf("other session prefs: %d, want 423", c)
	}
	// Watching and reading your own account still work.
	if c := call("other-token", "GET", "/api/me/prefs", ""); c != http.StatusOK {
		t.Fatalf("read prefs while locked: %d", c)
	}
	if c := call("other-token", "GET", "/api/home", ""); c != http.StatusOK {
		t.Fatalf("home while locked: %d", c)
	}
	// API keys are never locked.
	if c := call(key, "GET", "/api/admin/settings", ""); c != http.StatusOK {
		t.Fatalf("api key: %d", c)
	}

	if c := call("other-token", "POST", "/api/me/passcode/unlock", `{"passcode":"1111"}`); c != http.StatusForbidden {
		t.Fatalf("wrong passcode: %d", c)
	}
	if c := call("other-token", "POST", "/api/me/passcode/unlock", `{"password":"secret-password"}`); c != http.StatusOK {
		t.Fatalf("unlock with password: %d", c)
	}
	if c := call("other-token", "GET", "/api/admin/settings", ""); c != http.StatusOK {
		t.Fatalf("after unlock: %d", c)
	}
	if c := call("other-token", "POST", "/api/me/passcode/lock", ""); c != http.StatusNoContent {
		t.Fatalf("lock: %d", c)
	}
	if c := call("other-token", "GET", "/api/admin/settings", ""); c != http.StatusLocked {
		t.Fatalf("after lock: %d", c)
	}
	if c := call("other-token", "POST", "/api/me/passcode/unlock", `{"passcode":"2468"}`); c != http.StatusOK {
		t.Fatalf("unlock with passcode: %d", c)
	}
	if c := call("other-token", "DELETE", "/api/me/passcode", ""); c != http.StatusNoContent {
		t.Fatalf("remove: %d", c)
	}
	s.unlocks.lock("other-token")
	if c := call("other-token", "GET", "/api/admin/settings", ""); c != http.StatusOK {
		t.Fatalf("after removing the passcode: %d", c)
	}
}

func TestGuardedPath(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/admin/users", true},
		{"PUT", "/api/me/password", true},
		{"POST", "/api/link/approve", true},
		{"GET", "/api/me/prefs", false},
		{"PUT", "/api/me/pins", false},
		{"POST", "/api/me/passcode/unlock", false},
		{"POST", "/api/me/passcode/lock", false},
		{"POST", "/api/progress", false},
		{"POST", "/api/watchlist/3", false},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if got := guardedPath(req); got != tc.want {
			t.Errorf("%s %s guarded = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
