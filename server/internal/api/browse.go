package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/store"
)

type homeRow struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Items        []*store.Item `json:"items"`
	CollectionID int64         `json:"collectionId,omitempty"`
	LibraryID    int64         `json:"libraryId,omitempty"`
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	cw, err := s.continueWatching(uid, 20)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	rows := []homeRow{}
	add := func(id, title string, items []*store.Item, err error) {
		if err == nil && len(items) > 0 {
			rows = append(rows, homeRow{ID: id, Title: title, Items: items})
		}
	}
	if wl, err := s.Store.Watchlist(uid); err == nil {
		toWatch := []*store.Item{}
		for _, it := range wl {
			if (it.Kind == "movie" && !it.Watch.Watched) || (it.Kind == "show" && it.UnwatchedCount > 0) {
				toWatch = append(toWatch, it)
			}
		}
		add("watchlist", "On Your Watchlist", toWatch, nil)
	}
	recommended, recommendedBy := s.recommendedRow(uid)
	add("recommended", "Recommended for You", recommended, nil)
	libRows := s.homeLibraryRows(currentUser(r))
	for _, lr := range libRows {
		rows = append(rows, homeRow{ID: lr.ID, Title: lr.Title, Items: lr.Items, CollectionID: lr.CollectionID, LibraryID: lr.LibraryID})
	}

	// Hero carousel: the newest titles that have artwork to show off.
	hero := []*store.Item{}
	for _, lr := range libRows {
		if strings.HasSuffix(lr.ID, "-recent") {
			for _, it := range lr.Items {
				if it.Backdrop != "" {
					hero = append(hero, it)
				}
			}
		}
	}
	sort.SliceStable(hero, func(i, j int) bool { return hero[i].AddedAt > hero[j].AddedAt })
	if len(hero) > 6 {
		hero = hero[:6]
	}

	writeJSON(w, http.StatusOK, map[string]any{"hero": hero, "continueWatching": cw, "rows": rows, "recommendedBy": recommendedBy})
}

// continueWatching merges in-progress items with the next episode of shows
// the user recently finished an episode of (Plex's "On Deck").
func (s *Server) continueWatching(uid int64, limit int) ([]*store.Item, error) {
	inProgress, err := s.Store.InProgress(uid, limit)
	if err != nil {
		return nil, err
	}
	activeShows := map[int64]bool{}
	for _, it := range inProgress {
		if it.ShowID != 0 {
			activeShows[it.ShowID] = true
		}
	}
	finished, err := s.Store.RecentlyFinishedShows(uid, limit)
	if err != nil {
		return nil, err
	}
	type entry struct {
		item *store.Item
		at   int64
	}
	var all []entry
	for _, it := range inProgress {
		all = append(all, entry{it, it.Watch.UpdatedAt})
	}
	for _, p := range finished {
		if activeShows[p.ShowID] {
			continue
		}
		next, err := s.Store.NextEpisode(uid, p.ShowID, p.Season, p.Episode)
		if errors.Is(err, store.ErrNotFound) || (err == nil && next.Watch.Watched) {
			continue
		}
		if err != nil {
			return nil, err
		}
		all = append(all, entry{next, p.UpdatedAt})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at > all[j].at })
	out := []*store.Item{}
	for _, e := range all {
		if len(out) == limit {
			break
		}
		out = append(out, e.item)
	}
	return out, nil
}

func (s *Server) libraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.Store.Libraries()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	u := currentUser(r)
	if !u.IsAdmin {
		visible := libs[:0]
		for _, l := range libs {
			if s.Store.CanSeeLibrary(u, l.ID) {
				l.Paths = nil
				visible = append(visible, l)
			}
		}
		libs = visible
	}
	if err := s.Store.PinLibraries(u.ID, libs); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, libs)
}

// pins says whether the user has picked what to pin yet, so apps can ask
// once after the first sign-in.
func (s *Server) pins(w http.ResponseWriter, r *http.Request) {
	chosen, err := s.Store.PinsChosen(currentUser(r).ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"chosen": chosen})
}

// setPins saves the user's sidebar: every library in order, pinned or not.
func (s *Server) setPins(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Libraries []store.LibraryPin `json:"libraries"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	pins := body.Libraries[:0]
	for _, p := range body.Libraries {
		if s.Store.CanSeeLibrary(u, p.ID) {
			pins = append(pins, p)
		}
	}
	if err := s.Store.SetLibraryPins(u.ID, pins); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) libraryItems(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	lib, err := s.Store.Library(id)
	if err == nil && !s.Store.CanSeeLibrary(currentUser(r), id) {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// The first page also carries the library's collections, unless the
	// library hides them; "collapse" shows them instead of their titles.
	var cols []*store.Collection
	var hide []int64
	if lib.CollectionMode != "hide" && (offset == 0 || lib.CollectionMode == "collapse") {
		if cols, err = s.Store.LibraryCollections(id); err != nil {
			writeStoreErr(w, err)
			return
		}
		shown := cols[:0]
		for _, c := range cols {
			if c.ItemCount > 0 {
				shown = append(shown, c)
			}
		}
		cols = shown
		if lib.CollectionMode == "collapse" {
			if hide, err = s.Store.CollectedItems(id); err != nil {
				writeStoreErr(w, err)
				return
			}
		}
	}
	items, total, err := s.Store.LibraryItems(currentUser(r).ID, id, q.Get("sort"), max(offset, 0), limit, hide)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	lib.Paths = nil
	resp := map[string]any{"library": lib, "items": items, "total": total}
	if offset == 0 && cols != nil {
		resp["collections"] = cols
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u := currentUser(r)
	it, err := s.Store.Item(u.ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	resp := map[string]any{"item": it}

	switch it.Kind {
	case "movie", "episode":
		files, err := s.Store.FilesForItem(it.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if !u.IsAdmin {
			for _, f := range files {
				f.Path = filepath.Base(f.Path)
			}
		}
		resp["files"] = files
		resp["canDownload"] = s.canDownload(u)
		if ms, err := s.Store.Markers(it.ID); err == nil {
			resp["markers"] = ms
		}
	case "show", "season":
		children, err := s.Store.Children(u.ID, it.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		resp["children"] = children
	}

	if it.Kind == "movie" || it.Kind == "show" {
		if er, err := s.Store.ExternalRatings(it.ID); err == nil {
			if !er.Empty() {
				resp["externalRatings"] = er
			}
		} else if it.TMDBID > 0 {
			s.fetchRatingsSoon(it)
		}
	}
	if it.Kind == "show" {
		if id, order, err := s.Store.ShowTVDB(it.ID); err == nil && id > 0 {
			resp["tvdbId"], resp["tvdbOrder"] = id, order
		}
		if next, err := s.upNext(u.ID, it.ID); err == nil && next != nil {
			resp["upNext"] = next
		}
	}
	if it.Kind == "season" || it.Kind == "episode" {
		if show, err := s.Store.Item(u.ID, it.ShowID); err == nil {
			resp["show"] = show
		}
	}
	if it.Kind == "episode" {
		if season, err := s.Store.Item(u.ID, it.ParentID); err == nil {
			resp["season"] = season
		}
	}
	if it.Kind == "movie" || it.Kind == "show" {
		type shelf struct {
			Collection *store.Collection `json:"collection"`
			Items      []*store.Item     `json:"items"`
		}
		shelves := []shelf{}
		cols, _ := s.Store.ItemCollections(it.ID)
		for _, c := range cols {
			if members, err := s.Store.CollectionMembers(u.ID, c.ID); err == nil && len(members) > 1 {
				shelves = append(shelves, shelf{c, members})
			}
		}
		resp["collections"] = shelves
		if u.IsAdmin {
			resp["collectionNames"], _ = s.Store.ItemCollectionNames(it.ID)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// upNext picks the episode the Play button on a show should start: one in
// progress, else the one after the last watched, else the first.
func (s *Server) upNext(uid, showID int64) (*store.Item, error) {
	eps, err := s.Store.ShowEpisodes(uid, showID)
	if err != nil || len(eps) == 0 {
		return nil, err
	}
	var latest *store.Item
	for _, e := range eps {
		if e.Watch.Position > 15 && !e.Watch.Watched {
			return e, nil
		}
		if e.Watch.Watched && (latest == nil || e.Watch.UpdatedAt > latest.Watch.UpdatedAt) {
			latest = e
		}
	}
	if latest != nil {
		for i, e := range eps {
			if e.ID == latest.ID {
				for _, n := range eps[i+1:] {
					if !n.Watch.Watched {
						return n, nil
					}
				}
			}
		}
	}
	for _, e := range eps {
		if !e.Watch.Watched && e.SeasonNumber != nil && *e.SeasonNumber > 0 {
			return e, nil
		}
	}
	return eps[0], nil
}

func (s *Server) nextEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	uid := currentUser(r).ID
	it, err := s.Store.Item(uid, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if it.Kind != "episode" || it.SeasonNumber == nil || it.EpisodeNumber == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	next, err := s.Store.NextEpisode(uid, it.ShowID, *it.SeasonNumber, *it.EpisodeNumber)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, next)
}

func (s *Server) setWatched(watched bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r, "id")
		if !ok {
			writeErr(w, http.StatusBadRequest, "bad id")
			return
		}
		if _, err := s.Store.Item(currentUser(r).ID, id); err != nil {
			writeStoreErr(w, err)
			return
		}
		ids, err := s.Store.LeafIDs(id)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		if err := s.Store.SetWatched(currentUser(r).ID, ids, watched); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	items, err := s.Store.Search(currentUser(r).ID, q, 60)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) tmdbImage(w http.ResponseWriter, r *http.Request) {
	path, err := s.Images.TMDB(r.Context(), r.URL.Query().Get("w"), chi.URLParam(r, "file"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=2592000")
	http.ServeFile(w, r, path)
}

// fetchRatingsSoon looks up a title's external scores in the background the
// first time someone opens it, so they show up without waiting for the task.
func (s *Server) fetchRatingsSoon(it *store.Item) {
	if s.Metadata == nil || !s.Metadata.RatingsEnabled() {
		return
	}
	if _, busy := s.ratingFetches.LoadOrStore(it.ID, true); busy {
		return
	}
	go func() {
		defer s.ratingFetches.Delete(it.ID)
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		if err := s.Metadata.RefreshRatings(ctx, store.RatingTarget{ID: it.ID, Kind: it.Kind, TMDBID: it.TMDBID}); err != nil {
			log.Printf("ratings: %q: %v", it.Title, err)
		}
	}()
}
