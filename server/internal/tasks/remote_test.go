package tasks

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"bingebandit/internal/s3"
)

// fakeS3 is a bucket in memory, path-style, enough for the backup code.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		http.Error(w, "unsigned", http.StatusForbidden)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/bucket/")
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		type content struct {
			Key  string
			Size int
		}
		var res struct {
			XMLName  xml.Name `xml:"ListBucketResult"`
			Contents []content
		}
		prefix := r.URL.Query().Get("prefix")
		for k, v := range f.objects {
			if strings.HasPrefix(k, prefix) {
				res.Contents = append(res.Contents, content{k, len(v)})
			}
		}
		sort.Slice(res.Contents, func(i, j int) bool { return res.Contents[i].Key < res.Contents[j].Key })
		xml.NewEncoder(w).Encode(res)
	case r.Method == http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		f.objects[key] = b
	case r.Method == http.MethodGet:
		w.Write(f.objects[key])
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func TestUploadBackupKeepsNewest(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{
		"bb/bingebandit-20261020-020000.db":    []byte("old"),
		"bb/bingebandit-20261021-020000.db":    []byte("old"),
		"bb/bingebandit-20261022-020000.db":    []byte("old"),
		"bb/notes.txt":                         []byte("not a backup"),
		"other/bingebandit-20261019-020000.db": []byte("another server"),
	}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	remote := &s3.Client{Endpoint: srv.URL, Bucket: "bucket", Prefix: "bb", AccessKey: "k", SecretKey: "s"}

	path := filepath.Join(t.TempDir(), "bingebandit-20261024-020000.db")
	os.WriteFile(path, []byte("new backup"), 0o644)
	removed, err := UploadBackup(context.Background(), remote, path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("removed %d, want 2", removed)
	}
	var left []string
	for k := range fake.objects {
		left = append(left, k)
	}
	sort.Strings(left)
	want := "bb/bingebandit-20261022-020000.db bb/bingebandit-20261024-020000.db bb/notes.txt other/bingebandit-20261019-020000.db"
	if strings.Join(left, " ") != want {
		t.Errorf("left %v", left)
	}
	if string(fake.objects["bb/bingebandit-20261024-020000.db"]) != "new backup" {
		t.Error("upload content wrong")
	}
}
