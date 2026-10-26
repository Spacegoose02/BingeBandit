package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

func TestPosterOverlays(t *testing.T) {
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

	do := func(method, path string, body []byte, ctype string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer t")
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	pngOf := func(w, h int, c color.Color) []byte {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				img.Set(x, y, c)
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, img)
		return buf.Bytes()
	}

	lib := &store.Library{Name: "Movies", Kind: "movies", Paths: []string{}}
	st.CreateLibrary(lib)
	id, _ := st.UpsertItem(&store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Dune", MatchKey: "movie:dune"})
	poster, err := art.Save(bytes.NewReader(pngOf(400, 600, color.RGBA{90, 120, 160, 255})))
	if err != nil {
		t.Fatal(err)
	}
	st.SetArtwork(id, "poster", poster)
	st.UpsertFile(&store.MediaFile{ItemID: id, Path: "/m/dune.mkv", Width: 3840, Height: 2160, HDR: true, ProbedAt: 1,
		Streams: []store.Stream{{Type: "video", Range: "dv"}, {Type: "audio", Codec: "truehd", Channels: 8, Profile: "Dolby TrueHD + Dolby Atmos"}}}, 1)
	st.SaveExternalRatings(id, &store.ExternalRatings{IMDb: 8.0, RT: 92})

	itemPoster := func() string {
		t.Helper()
		var out struct {
			Item store.Item `json:"item"`
		}
		json.Unmarshal(do("GET", "/api/items/"+strconv.FormatInt(id, 10), nil, "").Body.Bytes(), &out)
		return out.Item.Poster
	}
	if p := itemPoster(); !strings.HasPrefix(p, "/api/images/local/") {
		t.Fatalf("overlays off: poster %q", p)
	}

	if rec := do("PUT", "/api/admin/settings", []byte(`{"overlays_enabled":"true"}`), ""); rec.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body)
	}
	url := itemPoster()
	if !strings.HasPrefix(url, "/api/images/poster/"+strconv.FormatInt(id, 10)+"-") {
		t.Fatalf("overlays on: poster %q", url)
	}
	rec := do("GET", url+"?w=w342", nil, "")
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") == "" {
		t.Fatalf("poster: %d %s", rec.Code, rec.Body)
	}
	img, err := jpeg.Decode(rec.Body)
	if err != nil || img.Bounds().Dx() != 342 {
		t.Fatalf("poster image: %v %v", err, img)
	}
	if out := os.Getenv("BB_OVERLAY_SAMPLE"); out != "" {
		f, _ := os.Create(out)
		png.Encode(f, img)
		f.Close()
	}
	if cached, _ := filepath.Glob(filepath.Join(dir, "cache", "overlays", "342", "*.jpg")); len(cached) != 1 {
		t.Errorf("cached drawings: %v", cached)
	}
	// Another user's request for an item they can't see.
	if rec := do("GET", "/api/images/poster/999-x.jpg", nil, ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown item: %d", rec.Code)
	}

	// Upload an overlay of your own and change it.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "badge.png")
	fw.Write(pngOf(100, 40, color.RGBA{255, 0, 0, 255}))
	mw.Close()
	rec = do("POST", "/api/admin/overlays/image", body.Bytes(), mw.FormDataContentType())
	var up struct{ Ref, URL string }
	json.Unmarshal(rec.Body.Bytes(), &up)
	if rec.Code != http.StatusOK || !strings.HasPrefix(up.Ref, "local:/") {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	for body, want := range map[string]int{
		`{"name":"","image":"` + up.Ref + `","position":"center","size":30,"condition":"always","enabled":true}`:         http.StatusBadRequest,
		`{"name":"x","image":"tmdb:/a.jpg","position":"center","size":30,"condition":"always","enabled":true}`:           http.StatusBadRequest,
		`{"name":"x","image":"` + up.Ref + `","position":"middle","size":30,"condition":"always","enabled":true}`:        http.StatusBadRequest,
		`{"name":"x","image":"` + up.Ref + `","position":"center","size":30,"condition":"label:","enabled":true}`:        http.StatusBadRequest,
		`{"name":"x","image":"` + up.Ref + `","position":"center","size":300,"condition":"always","enabled":true}`:       http.StatusBadRequest,
		`{"name":"4K","image":"` + up.Ref + `","position":"bottom-right","size":30,"condition":"res:4k","enabled":true}`: http.StatusOK,
	} {
		if rec := do("POST", "/api/admin/overlays", []byte(body), ""); rec.Code != want {
			t.Errorf("%s: %d, want %d (%s)", body, rec.Code, want, rec.Body)
		}
	}
	if url2 := itemPoster(); url2 == url {
		t.Error("poster URL didn't change with a new overlay")
	}
	var list struct {
		Overlays   []store.PosterOverlay `json:"overlays"`
		Conditions []any                 `json:"conditions"`
	}
	json.Unmarshal(do("GET", "/api/admin/overlays", nil, "").Body.Bytes(), &list)
	if len(list.Overlays) != 1 || !strings.HasPrefix(list.Overlays[0].Image, "/api/images/local/") || len(list.Conditions) == 0 {
		t.Fatalf("list: %+v", list)
	}
	o := list.Overlays[0]
	// Editing keeps the image when the client sends its URL back.
	edit := `{"name":"4K badge","image":"` + o.Image + `","position":"top-left","size":20,"margin":2,"condition":"res:4k","enabled":true}`
	if rec := do("PUT", "/api/admin/overlays/"+strconv.FormatInt(o.ID, 10), []byte(edit), ""); rec.Code != http.StatusOK {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body)
	}
	rec = do("GET", itemPoster()+"?w=w185", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("poster with overlay: %d", rec.Code)
	}
	if rec := do("GET", "/api/admin/overlays/preview?resolution=true&ratings=true&overlay="+strconv.FormatInt(o.ID, 10), nil, ""); rec.Code != http.StatusOK || rec.Header().Get("X-Item-Id") != strconv.FormatInt(id, 10) {
		t.Errorf("preview: %d %s", rec.Code, rec.Body)
	}
	if rec := do("DELETE", "/api/admin/overlays/"+strconv.FormatInt(o.ID, 10), nil, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}

	// With every badge off and no overlays, the plain poster is sent.
	do("PUT", "/api/admin/settings", []byte(`{"overlay_resolution":"false","overlay_hdr":"false","overlay_audio":"false","overlay_ratings":"false"}`), "")
	rec = do("GET", itemPoster()+"?w=w342", nil, "")
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/api/images/local/") {
		t.Errorf("nothing to draw: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	do("PUT", "/api/admin/settings", []byte(`{"overlays_enabled":"false"}`), "")
	if p := itemPoster(); !strings.HasPrefix(p, "/api/images/local/") {
		t.Errorf("overlays off again: %q", p)
	}
}
