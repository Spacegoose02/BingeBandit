package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/discover"
	"bingebandit/internal/store"
)

// feedXML is a YouTube Atom feed with n videos, newest first.
func feedXML(title, prefix string, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/" xmlns="http://www.w3.org/2005/Atom">
 <link rel="self" href="http://www.youtube.com/feeds/videos.xml?playlist_id=x"/>
 <id>yt:playlist:x</id>
 <title>%s</title>
 <author><name>Someone</name></author>
 <published>2020-01-01T00:00:00+00:00</published>`, title)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `
 <entry>
  <id>yt:video:%[1]s%[2]d</id>
  <yt:videoId>%[1]s%[2]d</yt:videoId>
  <title>%[3]s trailer %[2]d</title>
  <link rel="alternate" href="https://www.youtube.com/watch?v=%[1]s%[2]d"/>
  <author><name>Someone</name></author>
  <published>2026-10-%02[4]dT12:00:00+00:00</published>
  <media:group>
   <media:title>%[3]s trailer %[2]d</media:title>
   <media:thumbnail url="https://i1.ytimg.com/vi/%[1]s%[2]d/hqdefault.jpg" width="480" height="360"/>
   <media:description>Coming soon.</media:description>
  </media:group>
 </entry>`, prefix, i, title, 24-i)
	}
	b.WriteString("\n</feed>")
	return b.String()
}

func TestDiscover(t *testing.T) {
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/@netflix":
			w.Write([]byte(`<html><link rel="canonical" href="https://www.youtube.com/channel/UCWOA1ZGywLbqmigxE4Qlvuw"></html>`))
		case r.URL.Path == "/feeds/videos.xml" && r.URL.Query().Get("channel_id") == "UCWOA1ZGywLbqmigxE4Qlvuw":
			w.Write([]byte(feedXML("Netflix", "nf", 3)))
		case r.URL.Path == "/feeds/videos.xml" && r.URL.Query().Get("playlist_id") == "PLScC8g4bqD47c-qHlsfhGH3j6Bg7jzFy-":
			w.Write([]byte(feedXML("Movieclips Trailers", "mc", 15)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer yt.Close()
	discover.BaseURL = yt.URL

	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("admin", string(hash), true)
	sam, _ := st.CreateUser("sam", string(hash), false)
	st.CreateSession("admin", admin.ID, "Web", "")
	st.CreateSession("sam", sam.ID, "Web", "")
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

	if c := call("sam", "POST", "/api/admin/discover", `{"url":"@netflix"}`, nil); c != http.StatusForbidden {
		t.Fatalf("non-admin add: %d", c)
	}
	for _, bad := range []string{"https://vimeo.com/123", "not a link", "https://www.youtube.com/watch?v=abc"} {
		if c := call("admin", "POST", "/api/admin/discover", `{"url":"`+bad+`"}`, nil); c != http.StatusBadRequest {
			t.Errorf("%q: %d", bad, c)
		}
	}
	if c := call("admin", "POST", "/api/admin/discover", `{"url":"https://www.youtube.com/playlist?list=PLdoesnotexist123"}`, nil); c != http.StatusBadGateway {
		t.Errorf("missing playlist: %d", c)
	}

	var nf, mc store.DiscoverSource
	if c := call("admin", "POST", "/api/admin/discover", `{"url":"https://www.youtube.com/@netflix/videos"}`, &nf); c != http.StatusCreated ||
		nf.Name != "Netflix" || nf.Kind != "channel" || nf.YouTubeID != "UCWOA1ZGywLbqmigxE4Qlvuw" {
		t.Fatalf("add handle: %d %+v", c, nf)
	}
	if c := call("admin", "POST", "/api/admin/discover", `{"url":"youtube.com/watch?v=x&list=PLScC8g4bqD47c-qHlsfhGH3j6Bg7jzFy-","name":"New movies"}`, &mc); c != http.StatusCreated ||
		mc.Name != "New movies" || mc.Kind != "playlist" {
		t.Fatalf("add playlist: %d %+v", c, mc)
	}
	if c := call("admin", "POST", "/api/admin/discover", `{"url":"UCWOA1ZGywLbqmigxE4Qlvuw"}`, nil); c != http.StatusConflict {
		t.Errorf("duplicate: %d", c)
	}

	// Everyone sees the rows, newest video first, in the admin's order.
	call("admin", "PUT", "/api/admin/discover/order", fmt.Sprintf(`{"ids":[%d,%d]}`, mc.ID, nf.ID), nil)
	var rows []struct {
		Name   string
		Videos []store.DiscoverVideo
	}
	call("sam", "GET", "/api/discover", "", &rows)
	if len(rows) != 2 || rows[0].Name != "New movies" || len(rows[0].Videos) != 15 || rows[1].Videos[0].ID != "nf0" ||
		rows[1].Videos[0].Title != "Netflix trailer 0" || rows[1].Videos[0].Description != "Coming soon." {
		t.Fatalf("rows: %+v", rows)
	}

	// A refresh that fails keeps the videos and records the error.
	call("admin", "PUT", fmt.Sprintf("/api/admin/discover/%d", nf.ID), `{"name":"Netflix trailers"}`, nil)
	yt.Config.Handler = http.NotFoundHandler()
	var srcs []store.DiscoverSource
	call("admin", "POST", "/api/admin/discover/refresh", "", &srcs)
	if len(srcs) != 2 || srcs[1].Name != "Netflix trailers" || srcs[1].Error == "" {
		t.Fatalf("after failed refresh: %+v", srcs)
	}
	call("sam", "GET", "/api/discover", "", &rows)
	if len(rows) != 2 {
		t.Fatalf("videos lost after a failed refresh: %+v", rows)
	}

	if c := call("admin", "DELETE", fmt.Sprintf("/api/admin/discover/%d", mc.ID), "", nil); c != http.StatusNoContent {
		t.Fatalf("delete: %d", c)
	}
	call("sam", "GET", "/api/discover", "", &rows)
	if len(rows) != 1 || rows[0].Name != "Netflix trailers" {
		t.Fatalf("after delete: %+v", rows)
	}
}
