package importer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCinemaskCollections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/collections":
			w.Write([]byte(`[{"id":7,"name":"Pixar","itemCount":2}]`))
		case "/api/collections/7":
			w.Write([]byte(`{"id":7,"name":"Pixar","description":"Lamps","poster":"/lamp.jpg","plexLabel":"Pixar","fromLabel":"Pixar Films","items":[{"kind":"movie","tmdbId":862,"title":"Toy Story","year":1995},{"kind":"show","tmdbId":1,"title":"Bluey"},{"kind":"book","tmdbId":2}]}`))
		case "/api/images":
			w.Write([]byte("img:" + r.URL.Query().Get("src")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if _, err := NewCinemask(srv.URL, "wrong").Users(context.Background()); err == nil {
		t.Error("a bad key should fail to connect")
	}
	c := NewCinemask(srv.URL, "k")
	if _, err := c.Users(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []Collection
	if err := c.Collections(context.Background(), func(col Collection) error { got = append(got, col); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Pixar" || got[0].Summary != "Lamps" || len(got[0].Items) != 2 {
		t.Fatalf("got %+v", got)
	}
	if it := got[0].Items[0]; it.Kind != "movie" || it.TMDB != 862 || it.Year != 1995 {
		t.Errorf("first item %+v", it)
	}
	if g := got[0]; g.Poster != "tmdb:/lamp.jpg" || g.Label != "Pixar Films" || g.ItemLabel != "Pixar" {
		t.Errorf("poster %q label %q item label %q", g.Poster, g.Label, g.ItemLabel)
	}
	// Covers Cinemask keeps on Plex come through its image proxy.
	body, err := c.Image(context.Background(), "plex:/library/collections/5/thumb/1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	body.Close()
	if string(b) != "img:plex:/library/collections/5/thumb/1" {
		t.Errorf("image %q", b)
	}
}
