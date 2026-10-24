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

func TestTranscodeSettingsValidation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	u, _ := st.CreateUser("admin", string(hash), true)
	st.CreateSession("t", u.ID, "Web", "")
	h := (&Server{Store: st}).Router(context.Background())
	put := func(body string) int {
		req := httptest.NewRequest("PUT", "/api/admin/settings", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for body, want := range map[string]int{
		`{"tonemap_algorithm":"mobius"}`:            http.StatusOK,
		`{"tonemap_algorithm":"bt2390"}`:            http.StatusBadRequest,
		`{"hw_device":"/dev/dri/renderD129"}`:       http.StatusOK,
		`{"hw_device":"1"}`:                         http.StatusOK,
		`{"hw_device":""}`:                          http.StatusOK,
		`{"hw_device":"-vf drawtext"}`:              http.StatusBadRequest,
		`{"hw_device":"/dev/dri/../../etc/passwd"}`: http.StatusBadRequest,
		`{"hw_decode":"true"}`:                      http.StatusOK,
	} {
		if got := put(body); got != want {
			t.Errorf("%s: %d, want %d", body, got, want)
		}
	}
}
