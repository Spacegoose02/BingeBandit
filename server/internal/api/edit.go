package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

// adminItem loads an item for an admin endpoint, writing the error response itself.
func (s *Server) adminItem(w http.ResponseWriter, r *http.Request) *store.Item {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	it, err := s.Store.Item(0, id)
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return it
}

type editReq struct {
	store.Details
	Collections *[]string `json:"collections"` // nil = leave unchanged
}

func (s *Server) editItem(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	var req editReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.EpisodeOrder != "" && req.EpisodeOrder != "desc" {
		writeErr(w, http.StatusBadRequest, "episode order must be empty or desc")
		return
	}
	req.Genres, req.Labels = cleanList(req.Genres), cleanList(req.Labels)
	if err := s.Store.UpdateDetails(it.ID, &req.Details); err != nil {
		writeStoreErr(w, err)
		return
	}
	if req.Collections != nil && (it.Kind == "movie" || it.Kind == "show") {
		if err := s.Store.SetItemCollections(it.ID, it.LibraryID, cleanList(*req.Collections)); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// cleanList trims entries and drops blanks and case-insensitive duplicates.
func cleanList(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	return out
}

func validArtworkType(t string) bool { return t == "poster" || t == "backdrop" || t == "logo" }

func (s *Server) artworkOptions(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	kind := r.URL.Query().Get("type")
	if !validArtworkType(kind) {
		writeErr(w, http.StatusBadRequest, "type must be poster, backdrop or logo")
		return
	}
	opts, err := s.Metadata.ArtworkOptions(r.Context(), it, kind)
	if errors.Is(err, metadata.ErrNoKey) {
		writeJSON(w, http.StatusOK, map[string]any{"options": []any{}, "notice": err.Error()})
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	type option struct {
		metadata.ImageOption
		URL string `json:"url"`
	}
	out := make([]option, 0, len(opts))
	for _, o := range opts {
		out = append(out, option{o, store.ImageURL(o.Ref)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": out})
}

// setArtwork applies a TMDB choice ({type, ref}) or downloads one ({type, url}).
func (s *Server) setArtwork(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	var req struct {
		Type string `json:"type"`
		Ref  string `json:"ref"`
		URL  string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil || !validArtworkType(req.Type) {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ref := req.Ref
	switch {
	case req.URL != "":
		u := strings.TrimSpace(req.URL)
		if asset := s.TPDb.PosterLink(u); asset != "" {
			u = asset // a ThePosterDB poster page: download its image
		}
		var err error
		if ref, err = s.Artwork.Download(r.Context(), u); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case !metadata.ValidTMDBRef(ref):
		writeErr(w, http.StatusBadRequest, "invalid image reference")
		return
	}
	if err := s.Store.SetArtwork(it.ID, req.Type, ref); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": store.ImageURL(ref)})
}

func (s *Server) uploadArtwork(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	kind := r.URL.Query().Get("type")
	if !validArtworkType(kind) {
		writeErr(w, http.StatusBadRequest, "type must be poster, backdrop or logo")
		return
	}
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
	if err := s.Store.SetArtwork(it.ID, kind, ref); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": store.ImageURL(ref)})
}

// tpdbSearch looks the item up on ThePosterDB (or opens a pasted ThePosterDB link).
func (s *Server) tpdbSearch(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	kind := "movie"
	if it.Kind == "show" || it.Kind == "season" || it.Kind == "episode" {
		kind = "show"
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	year := it.Year
	if q == "" {
		q = it.Title
		if it.ShowID != 0 && it.ShowID != it.ID { // a season or episode: search for its show
			if show, err := s.Store.Item(0, it.ShowID); err == nil {
				q, year = show.Title, show.Year
			}
		}
	} else {
		year = 0
	}
	titles, err := s.TPDb.Search(r.Context(), kind, q, year)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "titles": titles})
}

// tpdbThumb serves a small copy of a ThePosterDB poster for the picker.
func (s *Server) tpdbThumb(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "poster")
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, err := s.TPDb.Thumb(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeFile(w, r, path)
}

func (s *Server) localImage(w http.ResponseWriter, r *http.Request) {
	path, err := s.Artwork.Path(chi.URLParam(r, "file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Content-addressed names never change content.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}

func (s *Server) matchSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if kind != "movie" && kind != "show" {
		writeErr(w, http.StatusBadRequest, "kind must be movie or show")
		return
	}
	year, _ := strconv.Atoi(q.Get("year"))
	res, err := s.Metadata.Search(r.Context(), kind, strings.TrimSpace(q.Get("q")), year)
	if errors.Is(err, metadata.ErrNoKey) {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) matchItem(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	if it.Kind != "movie" && it.Kind != "show" {
		writeErr(w, http.StatusBadRequest, "only movies and shows can be matched")
		return
	}
	var req struct {
		TMDBID int64 `json:"tmdbId"`
	}
	if err := readJSON(r, &req); err != nil || req.TMDBID <= 0 {
		writeErr(w, http.StatusBadRequest, "tmdbId is required")
		return
	}
	if err := s.Metadata.Match(r.Context(), it.ID, req.TMDBID); err != nil {
		log.Printf("match %d → %d: %v", it.ID, req.TMDBID, err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setMarkers saves admin-entered intro/credits times for a movie or episode.
func (s *Server) setMarkers(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	if it.Kind != "movie" && it.Kind != "episode" {
		writeErr(w, http.StatusBadRequest, "markers belong to movies and episodes")
		return
	}
	var req struct {
		Markers []store.Marker `json:"markers"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	seen := map[string]bool{}
	for _, m := range req.Markers {
		if (m.Kind != "intro" && m.Kind != "credits") || seen[m.Kind] || m.Start < 0 || m.End <= m.Start {
			writeErr(w, http.StatusBadRequest, "each marker needs kind intro or credits and an end after its start")
			return
		}
		seen[m.Kind] = true
	}
	if err := s.Store.SetManualMarkers(it.ID, req.Markers); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// detectMarkers re-runs intro/credits detection for a show or season in the background.
func (s *Server) detectMarkers(w http.ResponseWriter, r *http.Request) {
	it := s.adminItem(w, r)
	if it == nil {
		return
	}
	id := it.ID
	if it.Kind == "episode" {
		id = it.ParentID
	}
	seasons, err := s.Store.SeasonIDs(id)
	if err != nil || len(seasons) == 0 {
		writeErr(w, http.StatusBadRequest, "detection runs on shows, seasons and episodes")
		return
	}
	go func() {
		total := 0
		for _, sid := range seasons {
			n, err := s.Markers.Season(s.ctx, sid)
			if err != nil {
				log.Printf("markers: season %d: %v", sid, err)
				return
			}
			total += n
		}
		log.Printf("markers: %q: set %d markers in %d seasons", it.Title, total, len(seasons))
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) libraryLabels(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	labels, err := s.Store.LibraryLabels(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) libraryCollections(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if !s.Store.CanSeeLibrary(currentUser(r), id) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	cols, err := s.Store.LibraryCollections(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !currentUser(r).IsAdmin {
		visible := cols[:0]
		for _, c := range cols {
			if c.ItemCount > 0 {
				visible = append(visible, c)
			}
		}
		cols = visible
	}
	writeJSON(w, http.StatusOK, cols)
}

func (s *Server) collection(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	c, err := s.Store.Collection(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !s.Store.CanSeeLibrary(currentUser(r), c.LibraryID) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	items, err := s.Store.CollectionMembers(currentUser(r).ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collection": c, "items": items})
}

type collectionReq struct {
	Name      string `json:"name"`
	Overview  string `json:"overview"`
	LabelRule string `json:"labelRule"`
}

func (c *collectionReq) clean() error {
	c.Name, c.LabelRule = strings.TrimSpace(c.Name), strings.TrimSpace(c.LabelRule)
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	libID, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req collectionReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := req.clean(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := s.Store.CreateCollection(libID, req.Name, req.LabelRule)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "a collection with that name already exists")
			return
		}
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req collectionReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := req.clean(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.UpdateCollection(id, req.Name, req.Overview, req.LabelRule); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeErr(w, http.StatusConflict, "a collection with that name already exists")
			return
		}
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteCollection(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setUserLibraries(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		All        bool    `json:"all"`
		LibraryIDs []int64 `json:"libraryIds"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetUserLibraries(id, req.All, req.LibraryIDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
