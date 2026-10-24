package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/store"
)

// backupWith makes a backup file holding an admin named "admin" and a user
// named name.
func backupWith(t *testing.T, name string) []byte {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	if _, err := st.CreateUser("admin", string(hash), true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(name, string(hash), false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bingebandit-20261024-120000.db")
	if err := st.Backup(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSetupRestore(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{Store: st, DataDir: dataDir}
	h := s.Router(context.Background())
	post := func(body []byte, csrf bool) int {
		req := httptest.NewRequest("POST", "/api/setup/restore", bytes.NewReader(body))
		if csrf {
			req.Header.Set("X-Requested-With", "BingeBandit")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	backup := backupWith(t, "olduser")
	if c := post(backup, false); c != http.StatusForbidden {
		t.Fatalf("without X-Requested-With: %d", c)
	}
	if c := post([]byte("not a database"), true); c != http.StatusBadRequest {
		t.Fatalf("garbage: %d", c)
	}
	if c := post(backup, true); c != http.StatusOK {
		t.Fatalf("restore on a new server: %d", c)
	}
	if _, err := st.UserByUsername("olduser"); err != nil {
		t.Fatalf("restored user missing: %v", err)
	}
	// Once there are users, setup restore is closed.
	if c := post(backup, true); c != http.StatusConflict {
		t.Fatalf("second setup restore: %d", c)
	}
	// No temporary uploads are left behind.
	if m, _ := filepath.Glob(filepath.Join(dataDir, "upload-*")); len(m) != 0 {
		t.Fatalf("leftover uploads: %v", m)
	}
}

func TestUploadRestoreAndDownload(t *testing.T) {
	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "bb.db"))
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
	s := &Server{Store: st, DataDir: dataDir}
	h := s.Router(context.Background())
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer web-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if c := call("POST", "/api/admin/backups/upload", []byte("nope")).Code; c != http.StatusBadRequest {
		t.Fatalf("garbage upload: %d", c)
	}
	if c := call("POST", "/api/admin/backups/upload", backupWith(t, "olduser")).Code; c != http.StatusOK {
		t.Fatalf("upload restore: %d", c)
	}
	if _, err := st.UserByUsername("olduser"); err != nil {
		t.Fatalf("restored user missing: %v", err)
	}
	// The admin doing the restore is the same admin in the backup, so they
	// stay signed in. The data from before was saved first, and can be downloaded.
	saved, _ := filepath.Glob(filepath.Join(dataDir, "backups", "bingebandit-*.db"))
	if len(saved) != 1 {
		t.Fatalf("safety backups: %v", saved)
	}
	name := filepath.Base(saved[0])
	rec := call("GET", "/api/admin/backups/"+name+"/download", nil)
	if rec.Code != http.StatusOK || !bytes.HasPrefix(rec.Body.Bytes(), sqliteMagic) {
		t.Fatalf("download: %d", rec.Code)
	}
	if c := call("GET", "/api/admin/backups/..%2Fbb.db/download", nil).Code; c != http.StatusBadRequest {
		t.Fatalf("path escape: %d", c)
	}
}
