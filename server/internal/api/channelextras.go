package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

// ---- Logo library (admin) ----------------------------------------------------

type logoOut struct {
	*store.ChannelLogo
	URL string `json:"url"`
}

func libraryLogoURL(file string) string { return "/api/live/logos/" + file }

func (s *Server) adminLogos(w http.ResponseWriter, r *http.Request) {
	logos, err := s.Store.ChannelLogos()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := make([]logoOut, 0, len(logos))
	for _, l := range logos {
		out = append(out, logoOut{l, libraryLogoURL(l.File)})
	}
	writeJSON(w, http.StatusOK, out)
}

// uploadLogos adds one or more images ("file" fields) to the library, named
// after their file names. With match=1, each is also given to the channels
// whose names match it.
func (s *Server) uploadLogos(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "attach images as \"file\" fields (up to 2 MB each)")
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		writeErr(w, http.StatusBadRequest, "attach at least one image")
		return
	}
	var added []*store.ChannelLogo
	var failed []string
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			failed = append(failed, fh.Filename+": "+err.Error())
			continue
		}
		file, err := s.LogoLib.Save(f)
		f.Close()
		if err != nil {
			failed = append(failed, fh.Filename+": "+err.Error())
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(filepath.Base(fh.Filename), filepath.Ext(fh.Filename)))
		if name == "" {
			name = "Logo"
		}
		l, err := s.Store.AddChannelLogo(name, file)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		added = append(added, l)
	}
	matched := 0
	if r.FormValue("match") == "1" && len(added) > 0 {
		var err error
		if matched, err = s.matchLogos(added); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": len(added), "matched": matched, "failed": failed})
}

// matchLogos gives each logo to the channels whose names match its name.
// Returns how many channels changed.
func (s *Server) matchLogos(logos []*store.ChannelLogo) (int, error) {
	chans, err := s.Store.Channels(0, true)
	if err != nil {
		return 0, err
	}
	byName := map[string][]*store.Channel{}
	for _, c := range chans {
		if k := livetv.MatchName(c.Name); k != "" {
			byName[k] = append(byName[k], c)
		}
	}
	changed := 0
	for _, l := range logos {
		ref := store.LibraryLogoPrefix + l.File
		var ids []int64
		for _, c := range byName[livetv.MatchName(l.Name)] {
			if c.Logo != ref {
				ids = append(ids, c.ID)
			}
		}
		if err := s.Store.SetChannelsLogo(ids, ref); err != nil {
			return changed, err
		}
		changed += len(ids)
	}
	return changed, nil
}

func (s *Server) matchAllLogos(w http.ResponseWriter, r *http.Request) {
	logos, err := s.Store.ChannelLogos()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	n, err := s.matchLogos(logos)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"matched": n})
}

func (s *Server) renameLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || !ok || strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a logo needs a name")
		return
	}
	if err := s.Store.RenameChannelLogo(id, strings.TrimSpace(req.Name)); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	l, err := s.Store.DeleteChannelLogo(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	s.LogoLib.Remove(l.File)
	w.WriteHeader(http.StatusNoContent)
}

// assignLogo makes the listed channels (and only those) use a library logo.
func (s *Server) assignLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	var req struct {
		ChannelIDs []int64 `json:"channelIds"`
	}
	if err := readJSON(r, &req); err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	l, err := s.Store.ChannelLogo(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.Store.SetLogoChannels(l.File, req.ChannelIDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// libraryLogo serves an image from the logo library.
func (s *Server) libraryLogo(w http.ResponseWriter, r *http.Request) {
	s.serveLibraryLogo(w, r, chi.URLParam(r, "file"))
}

func (s *Server) serveLibraryLogo(w http.ResponseWriter, r *http.Request, file string) {
	path, ctype, err := s.LogoLib.Path(file)
	if err != nil || !s.Store.HasChannelLogoFile(file) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	// Content-addressed names never change content.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	http.ServeFile(w, r, path)
}

// ---- Viewers' own categories ---------------------------------------------------

func (s *Server) liveCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.Store.ChannelCategories(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cats)
}

func (s *Server) saveCategory(w http.ResponseWriter, r *http.Request) {
	var c store.ChannelCategory
	if err := readJSON(r, &c); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	c.ID = 0
	if r.Method == http.MethodPut {
		id, ok := idParam(r, "id")
		if !ok {
			writeErr(w, http.StatusBadRequest, "bad id")
			return
		}
		c.ID = id
	}
	if strings.TrimSpace(c.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a category needs a name")
		return
	}
	if err := s.Store.SaveChannelCategory(currentUser(r).ID, &c); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteChannelCategory(currentUser(r).ID, id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) orderCategories(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.OrderChannelCategories(currentUser(r).ID, req.IDs); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setChannelCategory adds a channel to (POST) or removes it from (DELETE) a category.
func (s *Server) setChannelCategory(in bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cat, ok1 := idParam(r, "id")
		ch, ok2 := idParam(r, "channel")
		if !ok1 || !ok2 {
			writeErr(w, http.StatusBadRequest, "bad id")
			return
		}
		if err := s.Store.SetChannelInCategory(currentUser(r).ID, cat, ch, in); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// checkLogo makes sure a library logo given to a channel is still in the library.
func (s *Server) checkLogo(logo string) string {
	if file, ok := strings.CutPrefix(logo, store.LibraryLogoPrefix); ok && !s.Store.HasChannelLogoFile(file) {
		return "that logo is no longer in the logo library"
	}
	return ""
}
