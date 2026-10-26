package api

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // uploaded overlays
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	_ "golang.org/x/image/webp"

	"bingebandit/internal/media"
	"bingebandit/internal/metadata"
	"bingebandit/internal/overlay"
	"bingebandit/internal/store"
)

// Poster sizes the apps ask for (?w=), in pixels.
var posterWidths = map[string]int{"w92": 92, "w185": 185, "w342": 342, "w500": 500, "w780": 780, "w1280": 1000, "original": 1000}

var rePosterFile = regexp.MustCompile(`^(\d+)-[0-9a-z]+\.jpg$`)

// overlayState holds decoded overlay images and limits how many posters are
// drawn at once (a library page asks for dozens together).
type overlayState struct {
	once   sync.Once
	slots  chan struct{}
	mu     sync.Mutex
	images map[string]image.Image // "<id>@<updated>"
	probed sync.Map               // file ids re-probed for Dolby Vision / Atmos
}

func (o *overlayState) init() {
	o.once.Do(func() {
		o.slots = make(chan struct{}, 4)
		o.images = map[string]image.Image{}
	})
}

func (s *Server) overlayBadges() overlay.Badges {
	on := func(k string) bool { return s.Store.Setting(k) == "true" }
	return overlay.Badges{
		Resolution: on(store.SettingOverlayResolution), HDR: on(store.SettingOverlayHDR),
		Audio: on(store.SettingOverlayAudio), Ratings: on(store.SettingOverlayRatings),
	}
}

// overlayPoster serves a movie's or show's poster with its overlays drawn
// on (/api/images/poster/<id>-<tag>.jpg). Drawn posters are kept in
// <data>/cache/overlays/<width>/ until what they show changes.
func (s *Server) overlayPoster(w http.ResponseWriter, r *http.Request) {
	m := rePosterFile.FindStringSubmatch(chi.URLParam(r, "file"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	facts, err := s.Store.OverlayFacts(currentUser(r).ID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	size := r.URL.Query().Get("w")
	width, ok := posterWidths[size]
	if !ok {
		size, width = "w500", 500
	}
	if !store.OverlaysOn() || facts.Ref == "" {
		s.redirectPoster(w, r, facts.Ref, size)
		return
	}
	s.reprobeForOverlays(r.Context(), facts)
	custom, err := s.Store.PosterOverlays()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	plan := overlay.PlanFor(overlay.FactsFrom(facts), s.overlayBadges(), custom)
	if plan.Empty() {
		s.redirectPoster(w, r, facts.Ref, size)
		return
	}
	key := plan.Key(facts.Ref, width)
	dir := filepath.Join(s.DataDir, "cache", "overlays", strconv.Itoa(width))
	path := filepath.Join(dir, fmt.Sprintf("%d-%s.jpg", id, key))
	if _, err := os.Stat(path); err != nil {
		data, err := s.renderPoster(r.Context(), facts.Ref, size, width, plan)
		if err != nil {
			s.redirectPoster(w, r, facts.Ref, size)
			return
		}
		os.MkdirAll(dir, 0o755)
		// Older drawings of this poster are out of date now.
		if old, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%d-*.jpg", id))); len(old) > 0 {
			for _, p := range old {
				os.Remove(p)
			}
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil || os.Rename(tmp, path) != nil {
			os.Remove(tmp)
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(data)
			return
		}
	}
	// Not immutable: a new 4K copy or a new score changes the picture
	// without changing the URL.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", `"`+key+`"`)
	http.ServeFile(w, r, path)
}

// redirectPoster sends the plain poster when there's nothing to draw.
func (s *Server) redirectPoster(w http.ResponseWriter, r *http.Request, ref, size string) {
	u := store.ImageURL(ref)
	if u == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(u, "/api/images/") {
		u += "?w=" + size
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.Redirect(w, r, u, http.StatusFound)
}

// renderPoster draws plan on the poster ref at width.
func (s *Server) renderPoster(ctx context.Context, ref, size string, width int, plan overlay.Plan) ([]byte, error) {
	s.overlays.init()
	select {
	case s.overlays.slots <- struct{}{}:
		defer func() { <-s.overlays.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	base, err := s.loadImage(ctx, ref, size)
	if err != nil {
		return nil, err
	}
	imgs := overlay.Images{}
	for _, o := range plan.Custom {
		if img, err := s.overlayImage(o); err == nil {
			imgs[o.ID] = img
		}
	}
	out := overlay.Draw(base, width, plan, imgs)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// loadImage decodes a stored image reference (tmdb:/… or local:/…).
func (s *Server) loadImage(ctx context.Context, ref, size string) (image.Image, error) {
	var path string
	var err error
	switch {
	case strings.HasPrefix(ref, "tmdb:/"):
		if s.Images == nil {
			return nil, fmt.Errorf("no image cache")
		}
		// Not the request's context: a cancelled poster shouldn't fail others
		// waiting on the same download.
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		path, err = s.Images.TMDB(dctx, size, strings.TrimPrefix(ref, "tmdb:/"))
	case strings.HasPrefix(ref, "local:/"):
		path, err = s.Artwork.Path(strings.TrimPrefix(ref, "local:/"))
	default:
		return nil, fmt.Errorf("can't draw on %q", ref)
	}
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func (s *Server) overlayImage(o *store.PosterOverlay) (image.Image, error) {
	s.overlays.init()
	k := fmt.Sprintf("%d@%d", o.ID, o.UpdatedAt)
	s.overlays.mu.Lock()
	img, ok := s.overlays.images[k]
	s.overlays.mu.Unlock()
	if ok {
		return img, nil
	}
	img, err := s.loadImage(context.Background(), o.Image, "original")
	if err != nil {
		return nil, err
	}
	s.overlays.mu.Lock()
	if len(s.overlays.images) > 64 {
		s.overlays.images = map[string]image.Image{}
	}
	s.overlays.images[k] = img
	s.overlays.mu.Unlock()
	return img, nil
}

// reprobeForOverlays looks again at a movie's files that were analyzed
// before BingeBandit looked for Dolby Vision and Atmos (each file once per
// run). Shows' episodes are left to the "Detect Dolby Vision and Atmos" task.
func (s *Server) reprobeForOverlays(ctx context.Context, f *store.OverlayFacts) {
	if s.FFprobe == "" || f.Item.Kind != "movie" {
		return
	}
	for _, mf := range f.Files {
		if !overlay.NeedsProbe(mf) {
			continue
		}
		if _, done := s.overlays.probed.LoadOrStore(mf.ID, true); done {
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if media.Probe(pctx, s.FFprobe, mf) == nil {
			s.Store.UpdateProbe(mf)
		}
		cancel()
	}
}

// --- admin ---

func (s *Server) adminOverlays(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.PosterOverlays()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, o := range list {
		o.Image = store.ImageURL(o.Image)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"overlays": list, "conditions": overlay.Conditions, "positions": overlay.Positions,
	})
}

// uploadOverlayImage stores an image for an overlay (PNG with transparency
// works best) and returns its reference.
func (s *Server) uploadOverlayImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, metadata.MaxArtworkBytes+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "attach an image as the \"file\" field (max 20 MB)")
		return
	}
	defer file.Close()
	ref, err := s.Artwork.Save(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ref": ref, "url": store.ImageURL(ref)})
}

type overlayReq struct {
	Name      string `json:"name"`
	Image     string `json:"image"` // local:/… from the upload, or the overlay's current URL
	Position  string `json:"position"`
	Size      int    `json:"size"`
	Margin    int    `json:"margin"`
	Condition string `json:"condition"`
	Kind      string `json:"kind"`
	LibraryID int64  `json:"libraryId"`
	Enabled   bool   `json:"enabled"`
}

func (req *overlayReq) apply(o *store.PosterOverlay) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Condition = strings.TrimSpace(req.Condition)
	if req.Name == "" {
		return fmt.Errorf("give the overlay a name")
	}
	if img := strings.TrimPrefix(req.Image, "/api/images/local/"); img != req.Image {
		req.Image = "local:/" + img
	}
	if !strings.HasPrefix(req.Image, "local:/") {
		return fmt.Errorf("upload an image for the overlay")
	}
	if !overlay.ValidPosition(req.Position) {
		return fmt.Errorf("unknown position")
	}
	if req.Size < 1 || req.Size > 100 {
		return fmt.Errorf("size must be 1–100%% of the poster's width")
	}
	if req.Margin < 0 || req.Margin > 25 {
		return fmt.Errorf("margin must be 0–25%%")
	}
	if err := overlay.ValidCondition(req.Condition); err != nil {
		return err
	}
	if req.Kind != "" && req.Kind != "movie" && req.Kind != "show" {
		return fmt.Errorf("kind must be movie, show or empty")
	}
	o.Name, o.Image, o.Position, o.Size, o.Margin = req.Name, req.Image, req.Position, req.Size, req.Margin
	o.Condition, o.Kind, o.LibraryID, o.Enabled = req.Condition, req.Kind, req.LibraryID, req.Enabled
	return nil
}

func (s *Server) saveOverlay(w http.ResponseWriter, r *http.Request) {
	var req overlayReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	o := &store.PosterOverlay{}
	if id, ok := idParam(r, "id"); ok {
		var err error
		if o, err = s.Store.PosterOverlay(id); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	if err := req.apply(o); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.Artwork.Path(strings.TrimPrefix(o.Image, "local:/")); err != nil {
		writeErr(w, http.StatusBadRequest, "upload an image for the overlay")
		return
	}
	if err := s.Store.SavePosterOverlay(o); err != nil {
		writeStoreErr(w, err)
		return
	}
	o.Image = store.ImageURL(o.Image)
	writeJSON(w, http.StatusOK, o)
}

func (s *Server) deleteOverlay(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeletePosterOverlay(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) orderOverlays(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.OrderPosterOverlays(req.IDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// overlayPreview draws a title's poster with the badges asked for (so the
// settings page can show them before saving) and every saved overlay. Some
// of the title's facts can be overridden (?res=4k&range=dv&audio=atmos) to
// see badges the title doesn't earn. With no item, a movie with a poster is
// picked.
func (s *Server) overlayPreview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("item"), 10, 64)
	if id == 0 {
		var err error
		if id, err = s.Store.SamplePosterItem(); err != nil {
			writeErr(w, http.StatusNotFound, "add a movie or show with a poster to preview overlays")
			return
		}
	}
	facts, err := s.Store.OverlayFacts(0, id)
	if err != nil || facts.Ref == "" {
		writeErr(w, http.StatusNotFound, "that title has no poster")
		return
	}
	f := overlay.FactsFrom(facts)
	if v := q.Get("res"); v != "" {
		f.Resolution = v
	}
	if v := q.Get("range"); v != "" {
		f.Range = v
	}
	if v := q.Get("audio"); v != "" {
		f.Audio = v
	}
	on := func(k string) bool { return q.Get(k) == "true" }
	b := overlay.Badges{Resolution: on("resolution"), HDR: on("hdr"), Audio: on("audioBadge"), Ratings: on("ratings")}
	custom, err := s.Store.PosterOverlays()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if only, _ := strconv.ParseInt(q.Get("overlay"), 10, 64); only > 0 {
		// Previewing one overlay: draw it whatever its condition says.
		for _, o := range custom {
			if o.ID == only {
				c := *o
				c.Condition, c.Enabled, c.Kind, c.LibraryID = "always", true, "", 0
				custom = []*store.PosterOverlay{&c}
			}
		}
	}
	data, err := s.renderPoster(r.Context(), facts.Ref, "w500", 500, overlay.PlanFor(f, b, custom))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't draw the poster: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Item-Id", strconv.FormatInt(id, 10))
	w.Write(data)
}
