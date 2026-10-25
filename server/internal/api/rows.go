package api

import (
	"net/http"
	"sort"
	"strings"

	"bingebandit/internal/store"
)

const rowSize = 24

// libRow is a row of titles from a library, with the collection it shows
// when it is one, so apps can link to the whole collection.
type libRow struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Items        []*store.Item `json:"items"`
	CollectionID int64         `json:"collectionId,omitempty"`
	LibraryID    int64         `json:"libraryId,omitempty"`
}

// isFriend is true for people outside the admin's household: they get the
// "Friends' Home" rows.
func (s *Server) isFriend(u *store.User) bool {
	if u.IsAdmin {
		return false
	}
	if u.OwnerID != 0 {
		if owner, err := s.Store.UserByID(u.OwnerID); err == nil && owner.IsAdmin {
			return false
		}
	}
	return true
}

// pinnedLibraries are the libraries u can see and pinned, in their order.
func (s *Server) pinnedLibraries(u *store.User) ([]*store.Library, error) {
	libs, err := s.Store.Libraries()
	if err != nil {
		return nil, err
	}
	seen := libs[:0]
	for _, l := range libs {
		if s.Store.CanSeeLibrary(u, l.ID) {
			seen = append(seen, l)
		}
	}
	if err := s.Store.PinLibraries(u.ID, seen); err != nil {
		return nil, err
	}
	out := []*store.Library{}
	for _, l := range seen {
		if l.Pinned {
			out = append(out, l)
		}
	}
	return out, nil
}

// homeLibraryRows merges the rows of every pinned library that are on Home
// (or Friends' Home). The same built-in row from several libraries of one
// kind becomes one row, so all pinned movie libraries share "Recently Added
// Movies", and so do collections with the same name ("Halloween" in TV Shows
// and Kids TV is one row). Rows keep the order of the first library that has
// them.
func (s *Server) homeLibraryRows(u *store.User) []libRow {
	libs, err := s.pinnedLibraries(u)
	if err != nil {
		return nil
	}
	friend := s.isFriend(u)
	type slot struct {
		row  libRow
		key  string
		kind string
		libs []int64
		cols []int64
	}
	var slots []*slot
	merged := map[string]*slot{}
	for _, l := range libs {
		rows, err := s.Store.LibraryRows(l)
		if err != nil {
			continue
		}
		for _, r := range rows {
			if (friend && !r.Friends) || (!friend && !r.Home) {
				continue
			}
			if r.CollectionID != 0 {
				mk := l.Kind + "/collection/" + strings.ToLower(r.Title)
				if m, ok := merged[mk]; ok {
					m.cols = append(m.cols, r.CollectionID)
					continue
				}
				m := &slot{row: libRow{ID: r.Key, Title: r.Title, CollectionID: r.CollectionID, LibraryID: l.ID}, cols: []int64{r.CollectionID}}
				merged[mk] = m
				slots = append(slots, m)
				continue
			}
			mk := l.Kind + "/" + r.Key
			if m, ok := merged[mk]; ok {
				m.libs = append(m.libs, l.ID)
				continue
			}
			m := &slot{row: libRow{ID: l.Kind + "-" + r.Key, Title: r.Title}, key: r.Key, kind: l.Kind, libs: []int64{l.ID}}
			merged[mk] = m
			slots = append(slots, m)
		}
	}
	out := []libRow{}
	for _, sl := range slots {
		if len(sl.cols) > 1 {
			out = append(out, s.mergedCollectionRow(u.ID, sl.row, sl.cols)...)
			continue
		}
		out = append(out, s.fillRow(u.ID, sl.row, sl.key, sl.kind, sl.libs)...)
	}
	return out
}

// mergedCollectionRow is one row of the titles of same-named collections from
// several libraries, in release order, each title once.
func (s *Server) mergedCollectionRow(uid int64, row libRow, cols []int64) []libRow {
	seen := map[int64]bool{}
	var items []*store.Item
	for _, id := range cols {
		members, err := s.Store.CollectionMembers(uid, id)
		if err != nil {
			continue
		}
		for _, it := range members {
			if !seen[it.ID] {
				seen[it.ID] = true
				items = append(items, it)
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].ReleaseDate < items[j].ReleaseDate })
	if len(items) > rowSize {
		items = items[:rowSize]
	}
	row.Items = items
	return []libRow{row}
}

// fillRow loads a row's titles; "genres" turns into one row per top genre.
func (s *Server) fillRow(uid int64, row libRow, key, kind string, libs []int64) []libRow {
	var out []libRow
	add := func(r libRow, items []*store.Item, err error) {
		if err == nil && len(items) > 0 {
			r.Items = items
			out = append(out, r)
		}
	}
	switch {
	case row.CollectionID != 0:
		items, err := s.Store.CollectionMembers(uid, row.CollectionID)
		if len(items) > rowSize {
			items = items[:rowSize]
		}
		add(row, items, err)
	case key == "genres":
		genres, _ := s.Store.TopGenres(libs, 3)
		for _, g := range genres {
			r := row
			r.ID += "-" + strings.ToLower(g)
			r.Title = strings.Replace(row.Title, "(Genre)", g, 1)
			items, err := s.Store.RowItems(uid, libs, kind, key, g, rowSize)
			add(r, items, err)
		}
	default:
		items, err := s.Store.RowItems(uid, libs, kind, key, "", rowSize)
		add(row, items, err)
	}
	return out
}

// libraryHubs are the rows on a library's Recommended tab.
func (s *Server) libraryHubs(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u := currentUser(r)
	lib, err := s.Store.Library(id)
	if err == nil && !s.Store.CanSeeLibrary(u, id) {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	rows, err := s.Store.LibraryRows(lib)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	out := []libRow{}
	for _, row := range rows {
		if row.Library {
			out = append(out, s.fillRow(u.ID, libRow{ID: row.Key, Title: row.Title, CollectionID: row.CollectionID, LibraryID: lib.ID}, row.Key, lib.Kind, []int64{lib.ID})...)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// libraryRows is the admin's Manage Recommendations table for a library.
func (s *Server) libraryRows(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryParam(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.LibraryRows(lib)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) setLibraryRows(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.libraryParam(w, r)
	if !ok {
		return
	}
	var rows []store.LibraryRow
	if err := readJSON(r, &rows); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetLibraryRows(lib.ID, rows); err != nil {
		writeStoreErr(w, err)
		return
	}
	s.libraryRows(w, r)
}

// setCollectionVisibility is the "Visible on" menu of a collection.
func (s *Server) setCollectionVisibility(w http.ResponseWriter, r *http.Request) {
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
	lib, err := s.Store.Library(c.LibraryID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var v store.Visibility
	if err := readJSON(r, &v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.Store.SetRowVisibility(lib, store.CollectionRowKey(id), v); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// collectionVisibility maps each collection of lib to where it shows.
func (s *Server) collectionVisibility(lib *store.Library) map[int64]store.Visibility {
	out := map[int64]store.Visibility{}
	rows, _ := s.Store.LibraryRows(lib)
	for _, r := range rows {
		if r.CollectionID != 0 {
			out[r.CollectionID] = r.Visibility
		}
	}
	return out
}

func (s *Server) libraryParam(w http.ResponseWriter, r *http.Request) (*store.Library, bool) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil, false
	}
	lib, err := s.Store.Library(id)
	if err != nil {
		writeStoreErr(w, err)
		return nil, false
	}
	return lib, true
}
