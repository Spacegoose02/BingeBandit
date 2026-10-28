package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImageCacheHas(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != "/w342/abc.jpg" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("jpeg"))
	}))
	defer srv.Close()
	c := NewImageCache(t.TempDir())
	c.base = srv.URL + "/"
	if c.Has("w342", "abc.jpg") {
		t.Fatal("nothing is cached yet")
	}
	if _, err := c.TMDB(context.Background(), "w342", "abc.jpg"); err != nil {
		t.Fatal(err)
	}
	if !c.Has("w342", "abc.jpg") || c.Has("w780", "abc.jpg") {
		t.Fatal("only the downloaded size is cached")
	}
	if c.Has("w342", "../x.jpg") {
		t.Fatal("bad names are never cached")
	}
	c.TMDB(context.Background(), "w342", "abc.jpg")
	if hits != 1 {
		t.Fatalf("downloaded %d times, want once", hits)
	}
}
