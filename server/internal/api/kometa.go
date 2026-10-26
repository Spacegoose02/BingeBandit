package api

import (
	"context"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"bingebandit/internal/importer"
	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

// kometaBackground runs the slow part of an import; tests replace it.
var kometaBackground = func(f func()) { go f() }

// kometaResult is one collection of an imported Kometa file.
type kometaResult struct {
	importer.KometaCollection
	ID      int64            `json:"id,omitempty"`
	Exists  bool             `json:"exists"` // a collection of that name was already in the library
	Visible store.Visibility `json:"visible"`
}

// importKometa reads a Kometa collection file into a library's collections:
// POST /admin/libraries/{id}/kometa?dryRun=1&recommended=1 with the YAML as
// the body. Collections are matched by name, so importing again updates
// them. Each follows its file's lists, sorts like it, and shows where the
// file's visible_* settings say; recommended=1 also shows on the library's
// Recommended tab the ones the file doesn't say about. Posters download and
// lists sync in the background.
func (s *Server) importKometa(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryParam(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the file is too big (4 MB at most)")
		return
	}
	cols, err := importer.ParseKometa(body, lib.Kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	dry := r.URL.Query().Get("dryRun") == "1"
	recommended := r.URL.Query().Get("recommended") == "1"
	existing, err := s.Store.LibraryCollections(lib.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	byName := map[string]*store.Collection{}
	for _, c := range existing {
		byName[strings.ToLower(c.Name)] = c
	}
	vis := s.collectionVisibility(lib)

	out := make([]kometaResult, 0, len(cols))
	seen := map[string]bool{}
	for _, kc := range cols {
		if seen[strings.ToLower(kc.Name)] {
			continue // the same name twice: the first one wins
		}
		seen[strings.ToLower(kc.Name)] = true
		res := kometaResult{KometaCollection: kc}
		cur := byName[strings.ToLower(kc.Name)]
		if cur != nil {
			res.ID, res.Exists, res.Visible = cur.ID, true, vis[cur.ID]
		}
		set := func(p *bool, v *bool) {
			if p != nil {
				*v = *p
			}
		}
		if kc.VisibleLibrary == nil && recommended {
			res.Visible.Library = true
		}
		set(kc.VisibleLibrary, &res.Visible.Library)
		set(kc.VisibleHome, &res.Visible.Home)
		set(kc.VisibleShared, &res.Visible.Friends)
		out = append(out, res)
	}
	if dry {
		writeJSON(w, http.StatusOK, map[string]any{"collections": out})
		return
	}

	for i := range out {
		res := &out[i]
		if res.ID == 0 {
			c, err := s.Store.CreateCollection(lib.ID, res.Name, store.SmartRules{})
			if err != nil {
				writeStoreErr(w, err)
				return
			}
			res.ID = c.ID
		}
		c, err := s.Store.Collection(res.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		overview := c.Overview
		if res.Summary != "" {
			overview = res.Summary
		}
		mode := c.Mode
		if res.Mode != "" {
			mode = res.Mode
		}
		if err := s.Store.UpdateCollection(c.ID, c.Name, overview, mode, c.SmartRules); err != nil {
			writeStoreErr(w, err)
			return
		}
		for _, err := range []error{
			s.Store.SetCollectionSortTitle(c.ID, res.SortTitle),
			s.Store.SetCollectionSort(c.ID, res.Sort),
			s.Store.SetCollectionList(c.ID, strings.Join(res.Lists, "\n")),
		} {
			if err != nil {
				writeStoreErr(w, err)
				return
			}
		}
	}
	if err := s.placeKometaRows(lib, out); err != nil {
		writeStoreErr(w, err)
		return
	}
	kometaBackground(func() { s.finishKometa(out) })
	writeJSON(w, http.StatusOK, map[string]any{"collections": out})
}

// placeKometaRows sets where the imported collections show, and puts their
// rows in sort-title order within the places they hold among the library's
// rows, so Home and Recommended start out in the file's tiers.
func (s *Server) placeKometaRows(lib *store.Library, cols []kometaResult) error {
	rows, err := s.Store.LibraryRows(lib)
	if err != nil {
		return err
	}
	imported := map[string]*kometaResult{}
	for i := range cols {
		imported[store.CollectionRowKey(cols[i].ID)] = &cols[i]
	}
	var slots []int
	var mine []store.LibraryRow
	for i, row := range rows {
		if c := imported[row.Key]; c != nil {
			row.Visibility = c.Visible
			slots = append(slots, i)
			mine = append(mine, row)
		}
	}
	title := func(r store.LibraryRow) string {
		if c := imported[r.Key]; c.SortTitle != "" {
			return c.SortTitle
		}
		return imported[r.Key].Name
	}
	sort.SliceStable(mine, func(a, b int) bool { return store.PlexCompare(title(mine[a]), title(mine[b])) < 0 })
	for i, slot := range slots {
		rows[slot] = mine[i]
	}
	return s.Store.SetLibraryRows(lib.ID, rows)
}

// finishKometa downloads the imported collections' posters and syncs their
// lists, which can take a few minutes for a big file.
func (s *Server) finishKometa(cols []kometaResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	lists := importer.NewLists(s.Store)
	for _, c := range cols {
		for kind, link := range map[string]string{"poster": c.Poster, "backdrop": c.Backdrop} {
			if link == "" {
				continue
			}
			if err := s.setCollectionImage(ctx, c.ID, kind, link); err != nil {
				log.Printf("kometa import: %s %s: %v", c.Name, kind, err)
			}
		}
		if len(c.Lists) == 0 {
			continue
		}
		if col, err := s.Store.Collection(c.ID); err == nil {
			lists.Sync(ctx, col)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// setCollectionImage downloads an image (or a ThePosterDB poster page's
// image) as a collection's poster or backdrop.
func (s *Server) setCollectionImage(ctx context.Context, id int64, kind, link string) error {
	if asset := s.TPDb.PosterLink(link); asset != "" {
		link = asset
	}
	ref, err := s.Artwork.Download(ctx, link)
	if err != nil {
		return err
	}
	return s.Store.SetCollectionArtwork(id, kind, ref)
}

// setCollectionArtwork sets a collection's poster or backdrop from a link
// ({type, url}, PUT) or an uploaded image (multipart "file", POST ?type=).
func (s *Server) setCollectionArtwork(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, err := s.Store.Collection(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	var kind, ref string
	if r.Method == http.MethodPost {
		kind = r.URL.Query().Get("type")
		r.Body = http.MaxBytesReader(w, r.Body, metadata.MaxArtworkBytes+1<<20)
		file, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "attach an image as the \"file\" field (max 20 MB)")
			return
		}
		defer file.Close()
		if kind != "poster" && kind != "backdrop" {
			writeErr(w, http.StatusBadRequest, "type must be poster or backdrop")
			return
		}
		if ref, err = s.Artwork.Save(file); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.Store.SetCollectionArtwork(id, kind, ref); err != nil {
			writeStoreErr(w, err)
			return
		}
	} else {
		var req struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		}
		if err := readJSON(r, &req); err != nil || (req.Type != "poster" && req.Type != "backdrop") {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		kind = req.Type
		if strings.TrimSpace(req.URL) == "" {
			// No link: back to the first title's poster.
			if err := s.Store.SetCollectionArtwork(id, kind, ""); err != nil {
				writeStoreErr(w, err)
				return
			}
		} else if err := s.setCollectionImage(r.Context(), id, kind, strings.TrimSpace(req.URL)); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	c, err := s.Store.Collection(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}
