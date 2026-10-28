package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

func TestLocalImageResized(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	admin, _ := st.CreateUser("admin", string(hash), true)
	st.CreateSession("t", admin.ID, "Web", "")
	art := metadata.NewArtwork(filepath.Join(dir, "artwork"))
	h := (&Server{Store: st, Artwork: art, DataDir: dir}).Router(context.Background())

	img := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	ref, err := art.Save(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	url := store.ImageURL(ref)
	if !strings.HasPrefix(url, "/api/images/local/") {
		t.Fatalf("url %q", url)
	}
	width := func(q string) int {
		t.Helper()
		req := httptest.NewRequest("GET", url+q, nil)
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", q, rec.Code)
		}
		cfg, _, err := image.DecodeConfig(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Width
	}
	if w := width("?w=w342"); w != 342 {
		t.Fatalf("w342 gave %d wide", w)
	}
	if w := width("?w=w342"); w != 342 { // from the cache
		t.Fatalf("cached w342 gave %d wide", w)
	}
	if w := width(""); w != 1000 {
		t.Fatalf("no size gave %d wide", w)
	}
	if w := width("?w=original"); w != 1000 {
		t.Fatalf("original gave %d wide", w)
	}
}
