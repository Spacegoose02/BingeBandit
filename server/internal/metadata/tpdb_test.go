package metadata

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Trimmed from the shape of ThePosterDB's pages; the parser only relies on
// links to /posters/<id> and on poster ids in attributes or asset links.
const tpdbSearchPage = `<div class="row">
<a href="https://theposterdb.com/posters/633" class="text-white"><img src="/images/x.jpg"></a>
<a href="https://theposterdb.com/posters/633" class="text-white"><p><b>The Matrix</b> (1999)</p></a>
<a href="https://theposterdb.com/posters/2481370">The Matrix Resurrections (2021)</a>
<a href="https://theposterdb.com/posters/77">The Matrix &amp; Friends (N/A)</a>
<a href="https://theposterdb.com/search?term=the%20matrix&page=2">2</a>
</div>`

const tpdbTitlePage = `<title>The Matrix (1999) | ThePosterDB</title>
<div class="col"><div class="overlay rounded-poster" data-poster-id="1001" data-poster-type="Movie">
<img data-src="https://images.theposterdb.com/1001.jpg"></div>
<p class="p-0 mb-1 text-break">The Matrix (1999)</p>
<a href="https://theposterdb.com/user/RedHeadJedi">RedHeadJedi</a>
<a href="https://theposterdb.com/api/assets/1001">Download</a></div>
<div class="col"><div class="overlay" data-poster-id="1002"></div>
<p class="text-break">The Matrix – Textless</p>
<a href="https://theposterdb.com/user/dm247">dm247</a></div>`

func TestParseTPDbSearch(t *testing.T) {
	got := NewTPDb("", "").parseSearch(tpdbSearchPage)
	if len(got) != 3 {
		t.Fatalf("got %d titles, want 3: %+v", len(got), got)
	}
	if got[0].ID != 633 || got[0].Title != "The Matrix (1999)" || got[0].URL != "https://theposterdb.com/posters/633" {
		t.Errorf("first = %+v", got[0])
	}
	if got[2].Title != "The Matrix & Friends (N/A)" {
		t.Errorf("entities not decoded: %q", got[2].Title)
	}
}

func TestParseTPDbPosters(t *testing.T) {
	got := NewTPDb("", "").parsePosters(tpdbTitlePage)
	want := []TPDbPoster{
		{ID: 1001, Title: "The Matrix (1999)", Uploader: "RedHeadJedi", Download: "https://theposterdb.com/api/assets/1001"},
		{ID: 1002, Title: "The Matrix – Textless", Uploader: "dm247", Download: "https://theposterdb.com/api/assets/1002"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("poster %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if pageTitle(tpdbTitlePage) != "The Matrix (1999)" {
		t.Errorf("pageTitle = %q", pageTitle(tpdbTitlePage))
	}
}

func TestParseTPDbPosterLink(t *testing.T) {
	if got := NewTPDb("", "").PosterLink("https://theposterdb.com/poster/12345"); got != "https://theposterdb.com/api/assets/12345" {
		t.Errorf("poster link = %q", got)
	}
	for _, s := range []string{"https://theposterdb.com/posters/633", "https://example.com/poster/1", "https://theposterdb.com/poster/x"} {
		if got := NewTPDb("", "").PosterLink(s); got != "" {
			t.Errorf("PosterLink(%q) = %q, want none", s, got)
		}
	}
}

func TestShrink(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	if b := shrink(src, 342).Bounds(); b.Dx() != 342 || b.Dy() != 513 {
		t.Fatalf("shrunk to %v", b)
	}
	if b := shrink(src, 2000).Bounds(); b.Dx() != 1000 {
		t.Fatalf("small image was scaled: %v", b)
	}
}

// An empty search must encode as [] rather than null: the web picker counts it.
func TestTPDbSearchEmptyIsList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>No results</body></html>"))
	}))
	defer srv.Close()
	got, err := NewTPDb(srv.URL, t.TempDir()).Search(context.Background(), "movie", "Nothing Here", 0)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if string(b) != "[]" {
		t.Fatalf("got %s, want []", b)
	}
}
