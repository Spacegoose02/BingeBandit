package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bingebandit/internal/store"
)

func TestLinkAsProfile(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	admin, _ := st.CreateUser("michael", "", true)
	dana, _ := st.CreateUser("dana", "", false)
	sam, _ := st.CreateUser("sam", "", false)
	kids, err := st.CreateProfile(dana, "Kids")
	if err != nil {
		t.Fatal(err)
	}
	st.CreateSession("admin-token", admin.ID, "Web", "")
	st.CreateSession("dana-token", dana.ID, "Web", "")
	st.CreateSession("sam-token", sam.ID, "Web", "")
	st.CreateSession("kids-token", kids.ID, "Web", "")
	h := (&Server{Store: st}).Router(context.Background())

	call := func(token, method, path, body string, out any) int {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if out != nil {
			json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}
	type started struct{ Code, PollToken string }
	start := func() started {
		t.Helper()
		var s started
		if c := call("", "POST", "/api/link/start", `{"device":"Bedroom TV"}`, &s); c != http.StatusOK {
			t.Fatalf("start: %d", c)
		}
		return s
	}
	approve := func(token, code string, uid int64) int {
		t.Helper()
		return call(token, "POST", "/api/link/approve", `{"code":"`+code+`","userId":`+itoa(uid)+`}`, nil)
	}
	poll := func(s started) (int, map[string]any) {
		t.Helper()
		var out map[string]any
		c := call("", "GET", "/api/link/poll?token="+s.PollToken, "", &out)
		return c, out
	}

	// An account links a TV straight to one of its profiles.
	l := start()
	if c := approve("dana-token", l.Code, kids.ID); c != http.StatusOK {
		t.Fatalf("own profile: %d", c)
	}
	c, out := poll(l)
	if c != http.StatusOK || out["profileChosen"] != true || out["token"] == "" {
		t.Fatalf("poll = %d %+v", c, out)
	}
	if u := out["user"].(map[string]any); int64(u["id"].(float64)) != kids.ID {
		t.Fatalf("signed in as %v", u["id"])
	}

	// Approving as yourself keeps "Who's watching?".
	l = start()
	approve("dana-token", l.Code, 0)
	if _, out := poll(l); out["profileChosen"] != false {
		t.Fatalf("account link = %+v", out)
	}

	// Not someone else, and a profile can't pick its account (that skips PINs).
	l = start()
	if c := approve("sam-token", l.Code, kids.ID); c != http.StatusForbidden {
		t.Fatalf("other account's profile: %d", c)
	}
	if c := approve("kids-token", l.Code, dana.ID); c != http.StatusForbidden {
		t.Fatalf("profile picking its account: %d", c)
	}
	if c := approve("sam-token", l.Code, dana.ID); c != http.StatusForbidden {
		t.Fatalf("other account: %d", c)
	}
	// Admins can set up anyone's TV.
	if c := approve("admin-token", l.Code, kids.ID); c != http.StatusOK {
		t.Fatalf("admin: %d", c)
	}

	var info map[string]string
	if c := call("dana-token", "GET", "/api/link/info", "", &info); c != http.StatusOK || info["serverUrl"] == "" {
		t.Fatalf("info = %d %+v", c, info)
	}
}
