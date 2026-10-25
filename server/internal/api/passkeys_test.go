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

// The ceremonies themselves were tried end to end in Chromium with a virtual
// authenticator; this covers the server's bookkeeping around them.
func TestPasskeyEndpoints(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("admin", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("admin-token", admin.ID, "Web", "")
	st.CreateSession("sam-token", sam.ID, "Web", "")
	h := (&Server{Store: st, DataDir: dir}).Router(context.Background())
	call := func(token, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://tv.example.com:8484"+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Registration options name this server and exclude nothing yet.
	rec := call("sam-token", "POST", "/api/me/passkeys/begin", `{}`)
	var begin struct {
		Flow    string `json:"flow"`
		Options struct {
			RP struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"options"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &begin) != nil {
		t.Fatalf("begin: %d %s", rec.Code, rec.Body)
	}
	if begin.Options.RP.ID != "tv.example.com" || begin.Options.User.Name != "sam" || begin.Flow == "" {
		t.Errorf("options = %+v", begin)
	}
	// Another user can't finish sam's registration, and a flow works once.
	if rec := call("admin-token", "POST", "/api/me/passkeys", `{"flow":"`+begin.Flow+`","credential":{}}`); rec.Code != http.StatusBadRequest {
		t.Errorf("finishing someone else's flow: %d", rec.Code)
	}
	if rec := call("sam-token", "POST", "/api/me/passkeys", `{"flow":"`+begin.Flow+`","credential":{}}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("reusing a flow: %d %s", rec.Code, rec.Body)
	}

	// The page's Origin is used when it is this server's address, not otherwise.
	rec = call("", "POST", "/api/auth/passkey/begin", `{}`, "Origin", "https://evil.example")
	if !strings.Contains(rec.Body.String(), `"rpId":"tv.example.com"`) {
		t.Errorf("foreign Origin changed the RP ID: %s", rec.Body)
	}
	if rec := call("admin-token", "PUT", "/api/admin/settings", `{"public_url":"https://bb.example.org"}`); rec.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	rec = call("", "POST", "/api/auth/passkey/begin", `{}`, "Origin", "https://bb.example.org")
	var login struct {
		Flow string `json:"flow"`
	}
	if !strings.Contains(rec.Body.String(), `"rpId":"bb.example.org"`) || json.Unmarshal(rec.Body.Bytes(), &login) != nil {
		t.Errorf("public URL origin: %s", rec.Body)
	}
	// A login begun on one address can't be finished on another.
	rec = call("", "POST", "/api/auth/passkey/finish", `{"flow":"`+login.Flow+`","credential":{}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "another address") {
		t.Errorf("finish on another address: %d %s", rec.Code, rec.Body)
	}

	// Listing, renaming, deleting (own only), and the admin's remove-all.
	k1 := &store.Passkey{UserID: sam.ID, CredentialID: []byte{1}, Credential: "{}", Name: "Phone"}
	k2 := &store.Passkey{UserID: sam.ID, CredentialID: []byte{2}, Credential: "{}", Name: "Laptop"}
	st.AddPasskey(k1)
	st.AddPasskey(k2)
	if rec := call("sam-token", "GET", "/api/me/passkeys", ""); !strings.Contains(rec.Body.String(), `"Laptop"`) || strings.Contains(rec.Body.String(), "credential") {
		t.Errorf("list: %s", rec.Body)
	}
	if rec := call("admin-token", "DELETE", "/api/me/passkeys/"+itoa(k1.ID), ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleting someone else's passkey: %d", rec.Code)
	}
	if rec := call("sam-token", "PUT", "/api/me/passkeys/"+itoa(k1.ID), `{"name":"Pixel"}`); rec.Code != http.StatusNoContent {
		t.Errorf("rename: %d", rec.Code)
	}
	users, _ := st.ListUsers()
	for _, u := range users {
		if u.ID == sam.ID && u.Passkeys != 2 {
			t.Errorf("sam has %d passkeys listed", u.Passkeys)
		}
	}
	if rec := call("admin-token", "DELETE", "/api/admin/users/"+itoa(sam.ID)+"/passkeys", ""); rec.Code != http.StatusNoContent {
		t.Errorf("admin remove: %d", rec.Code)
	}
	if keys, _ := st.Passkeys(sam.ID); len(keys) != 0 {
		t.Errorf("%d passkeys left", len(keys))
	}
}
