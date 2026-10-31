package opensubs

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// A fake OpenSubtitles: checks the key, the query and the sign-in, and hands
// out one file.
func fake(t *testing.T) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file/2" { // the download link needs no key
			fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:02,000\nHello\n")
			return
		}
		if r.Header.Get("Api-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"bad key"}`)
			return
		}
		switch r.URL.Path {
		case "/subtitles":
			q := r.URL.Query()
			if q.Get("parent_tmdb_id") != "1399" || q.Get("season_number") != "1" || q.Get("episode_number") != "2" || q.Get("languages") != "en" {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"data":[
				{"attributes":{"language":"en","download_count":900,"release":"Popular","files":[{"file_id":1}]}},
				{"attributes":{"language":"en","download_count":5,"release":"Exact","moviehash_match":true,"files":[{"file_id":2}]}},
				{"attributes":{"language":"en","download_count":5000,"release":"Robot","ai_translated":true,"files":[{"file_id":3}]}},
				{"attributes":{"language":"en","release":"No files","files":[]}}]}`)
		case "/login":
			fmt.Fprint(w, `{"token":"tok","base_url":"vip.example"}`)
		case "/download":
			if r.Header.Get("Authorization") != "Bearer tok" {
				t.Error("download without the token")
			}
			var in struct {
				FileID int64 `json:"file_id"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.FileID == 9 {
				w.WriteHeader(http.StatusNotAcceptable)
				fmt.Fprint(w, `{"message":"You have downloaded your allowed 20 subtitles"}`)
				return
			}
			fmt.Fprintf(w, `{"link":"%s/file/%d"}`, srv.URL, in.FileID)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchAndDownload(t *testing.T) {
	srv := fake(t)
	c := &Client{Base: srv.URL, UserAgent: "test", Key: func() string { return "k" }, Login: func() (string, string) { return "me", "pw" }}
	rs, err := c.Search(context.Background(), Query{Languages: []string{"en"}, TMDBID: 1399, Episode: true, Season: 1, Number: 2})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rs {
		names = append(names, r.Name)
	}
	// Made for this file first, then by downloads, machine translations last.
	if fmt.Sprint(names) != "[Exact Popular Robot]" {
		t.Errorf("order %v", names)
	}
	b, err := c.Download(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "1\n00:00:01,000 --> 00:00:02,000\nHello\n" {
		t.Errorf("got %q", b)
	}
	_, err = c.Download(context.Background(), 9)
	if e, ok := err.(*Error); !ok || e.Status != http.StatusNotAcceptable {
		t.Errorf("used-up downloads: %v", err)
	}

	c.Key = func() string { return "" }
	if _, err := c.Search(context.Background(), Query{}); err != ErrNoKey {
		t.Errorf("no key: %v", err)
	}
	c.Key = func() string { return "wrong" }
	if _, err := c.Search(context.Background(), Query{}); err == nil {
		t.Error("wrong key accepted")
	}
}

func TestHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.mkv")
	b := make([]byte, 200*1024)
	// First chunk sums to 1+2, last chunk (overlapping nothing) to 3.
	binary.LittleEndian.PutUint64(b[0:], 1)
	binary.LittleEndian.PutUint64(b[8:], 2)
	binary.LittleEndian.PutUint64(b[len(b)-8:], 3)
	os.WriteFile(path, b, 0o644)
	if got, want := Hash(path), fmt.Sprintf("%016x", uint64(len(b))+6); got != want {
		t.Errorf("hash %s, want %s", got, want)
	}
	os.WriteFile(path, b[:1000], 0o644)
	if Hash(path) != "" {
		t.Error("small file hashed")
	}
}
