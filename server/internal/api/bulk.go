package api

import (
	"net/http"
	"strconv"

	"bingebandit/internal/store"
)

// bulkEdit applies one change to many titles or collections, from the web's
// multi-select editor: movies and shows can go in or out of collections and
// gain or lose labels; collections only have labels.
func (s *Server) bulkEdit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemIDs       []int64 `json:"itemIds"`
		CollectionIDs []int64 `json:"collectionIds"`
		store.BulkEdit
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(req.ItemIDs)+len(req.CollectionIDs) > 5000 {
		writeErr(w, http.StatusBadRequest, "too many at once")
		return
	}
	e := req.BulkEdit
	e.AddCollections, e.RemoveCollections = cleanList(e.AddCollections), cleanList(e.RemoveCollections)
	e.AddLabels, e.RemoveLabels = cleanList(e.AddLabels), cleanList(e.RemoveLabels)
	items, err := s.Store.BulkEditItems(req.ItemIDs, e)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	cols, err := s.Store.BulkEditCollections(req.CollectionIDs, e)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"items": items, "collections": cols})
}

// collectionLabels lists the labels used on collections, in one library
// (?library=) or all of them, for autocomplete.
func (s *Server) collectionLabels(w http.ResponseWriter, r *http.Request) {
	lib, _ := strconv.ParseInt(r.URL.Query().Get("library"), 10, 64)
	labels, err := s.Store.CollectionLabels(lib)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}
