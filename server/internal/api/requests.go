package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

// Title requests: people ask for a movie or show the server doesn't have,
// an admin approves it, and a plugin that lists request.approved in its
// manifest (Cinemask Upgrades) fetches it and reports back through the
// plugin API. Once the title shows up in a library, the people who asked
// hear about it.

// requestsOn is the admin's switch for title requests.
func (s *Server) requestsOn() bool {
	return s.Store.Setting(store.SettingRequests) != "false"
}

// canRequest says why u can't ask for titles, or "" when they can. Kids
// can't: search would show them anything TMDB has.
func (s *Server) canRequest(u *store.User) string {
	switch {
	case !s.requestsOn():
		return "an admin turned requests off"
	case u.Kid:
		return "kids can't request titles"
	}
	return ""
}

// requestSearchResult is a TMDB title with what the server knows of it.
type requestSearchResult struct {
	metadata.SearchResult
	Kind     string              `json:"kind"`
	ItemID   int64               `json:"itemId,omitempty"`   // in a library the viewer can see
	OnServer bool                `json:"onServer,omitempty"` // in a library, maybe one the viewer can't see
	Request  *store.TitleRequest `json:"request,omitempty"`
}

// searchRequestable searches TMDB for movies and shows (or one kind).
func (s *Server) searchRequestable(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if why := s.canRequest(u); why != "" {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []requestSearchResult{})
		return
	}
	kinds := []string{"movie", "show"}
	if k := r.URL.Query().Get("kind"); k == "movie" || k == "show" {
		kinds = []string{k}
	}
	if s.Metadata == nil || !s.Metadata.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "Requests need a TMDB API key; ask your server admin to add one in Settings")
		return
	}
	found := make([][]metadata.SearchResult, len(kinds))
	errs := make([]error, len(kinds))
	var wg sync.WaitGroup
	for i, kind := range kinds {
		wg.Go(func() { found[i], errs[i] = s.Metadata.Search(r.Context(), kind, q, 0) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// TMDB lists the best matches first; alternate movies and shows so the
	// best of each come first.
	var out []requestSearchResult
	for i := 0; i < 20; i++ {
		for k, list := range found {
			if i < len(list) && list[i].TMDBID > 0 {
				out = append(out, requestSearchResult{SearchResult: list[i], Kind: kinds[k]})
			}
		}
	}
	s.annotateRequestable(u, out)
	if out == nil {
		out = []requestSearchResult{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) annotateRequestable(u *store.User, list []requestSearchResult) {
	keys := make([]string, len(list))
	for i, it := range list {
		keys[i] = it.Kind + ":" + strconv.FormatInt(it.TMDBID, 10)
	}
	reqs, _ := s.Store.TitleRequestsFor(keys)
	for i := range list {
		it := &list[i]
		if mine, _ := s.Store.FindItems(u.ID, it.Kind, it.TMDBID, "", 0); len(mine) > 0 {
			it.ItemID, it.OnServer = mine[0].ID, true
		} else if all, _ := s.Store.FindItems(0, it.Kind, it.TMDBID, "", 0); len(all) > 0 {
			it.OnServer = true
		}
		if t := reqs[keys[i]]; t != nil {
			t.Mine = s.Store.RequestedBy(t.ID, u.ID)
			it.Request = t
		}
	}
}

// myRequests lists the viewer's requests, or for admins with ?all=1 every
// request, with whether requests are on and who fetches approved ones.
func (s *Server) myRequests(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	uid := u.ID
	if u.IsAdmin && r.URL.Query().Get("all") == "1" {
		uid = 0
	}
	list, err := s.Store.TitleRequests(uid)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, t := range list {
		t.Mine = uid == u.ID || s.Store.RequestedBy(t.ID, u.ID)
	}
	out := map[string]any{"requests": list, "enabled": s.requestsOn(), "canRequest": s.canRequest(u) == "", "reason": s.canRequest(u)}
	if u.IsAdmin {
		handlers := []string{}
		if s.Plugins != nil {
			handlers = s.Plugins.Handlers("request.approved")
		}
		out["handlers"] = handlers
	}
	writeJSON(w, http.StatusOK, out)
}

var requestPoster = regexp.MustCompile(`^/api/images/tmdb/([A-Za-z0-9_-]+\.(?:jpe?g|png|webp))$`)

// createRequest asks for a title from the request search. An admin's own
// request is approved at once.
func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if why := s.canRequest(u); why != "" {
		writeErr(w, http.StatusForbidden, why)
		return
	}
	var req struct {
		Kind     string `json:"kind"`
		TMDBID   int64  `json:"tmdbId"`
		Title    string `json:"title"`
		Year     int    `json:"year"`
		Poster   string `json:"poster"`
		Overview string `json:"overview"`
		Note     string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Title, req.Note = strings.TrimSpace(req.Title), strings.TrimSpace(req.Note)
	switch {
	case req.Kind != "movie" && req.Kind != "show":
		writeErr(w, http.StatusBadRequest, "kind must be movie or show")
		return
	case req.TMDBID <= 0:
		writeErr(w, http.StatusBadRequest, "tmdbId is required")
		return
	case req.Title == "" || utf8.RuneCountInString(req.Title) > 300:
		writeErr(w, http.StatusBadRequest, "a title is required")
		return
	case utf8.RuneCountInString(req.Note) > 500:
		writeErr(w, http.StatusBadRequest, "keep the note under 500 characters")
		return
	case req.Year < 0 || req.Year > 3000:
		req.Year = 0
	}
	if found, _ := s.Store.FindItems(0, req.Kind, req.TMDBID, "", 0); len(found) > 0 {
		writeErr(w, http.StatusConflict, "this title is already on the server")
		return
	}
	poster := ""
	if m := requestPoster.FindStringSubmatch(req.Poster); m != nil {
		poster = "tmdb:/" + m[1]
	}
	if utf8.RuneCountInString(req.Overview) > 2000 {
		req.Overview = string([]rune(req.Overview)[:2000])
	}
	status := store.RequestPending
	if u.IsAdmin {
		status = store.RequestApproved
	}
	t, created, err := s.Store.RequestTitle(u.ID, &store.TitleRequest{Kind: req.Kind, TMDBID: req.TMDBID, Title: req.Title,
		Year: req.Year, Poster: poster, Overview: req.Overview, Note: req.Note}, status)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	switch {
	case created && status == store.RequestApproved:
		s.Store.SetRequestStatus(t.ID, status, "", "", u.ID)
		s.Emit("request.approved", t)
	case created:
		s.Emit("request.created", t)
		s.notifyAdmins(t, u)
	case u.IsAdmin && t.Status == store.RequestPending:
		// An admin asking for something already asked for approves it.
		if err := s.Store.SetRequestStatus(t.ID, store.RequestApproved, "", "", u.ID); err == nil {
			t.Status = store.RequestApproved
			s.Emit("request.approved", t)
		}
	}
	t.Mine = true
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, t)
}

// notifyAdmins tells the admins (not the one asking) about a new request.
func (s *Server) notifyAdmins(t *store.TitleRequest, from *store.User) {
	users, err := s.Store.ListUsers()
	if err != nil {
		return
	}
	for _, a := range users {
		if !a.IsAdmin || a.OwnerID != 0 || a.ID == from.ID {
			continue
		}
		n := &store.Notification{UserID: a.ID, Kind: "request", Title: personName(from) + " requested " + yearTitle(t),
			Body: t.Note, Link: "/requests", Image: t.Poster}
		if err := s.Store.AddNotification(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

func personName(u *store.User) string {
	if u.ProfileName != "" {
		return u.ProfileName
	}
	return u.Username
}

func yearTitle(t *store.TitleRequest) string {
	if t.Year > 0 {
		return fmt.Sprintf("%s (%d)", t.Title, t.Year)
	}
	return t.Title
}

// requestParam loads the request named in the path.
func (s *Server) requestParam(w http.ResponseWriter, r *http.Request) *store.TitleRequest {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	t, err := s.Store.TitleRequest(id)
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return t
}

// deleteRequest takes the viewer's name off a request that isn't approved
// yet (dropping it when nobody else asked). Admins delete any request.
func (s *Server) deleteRequest(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	u := currentUser(r)
	if u.IsAdmin && r.URL.Query().Get("all") == "1" {
		if err := s.Store.DeleteTitleRequest(t.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.Store.RequestedBy(t.ID, u.ID) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if t.Status != store.RequestPending && t.Status != store.RequestDeclined && t.Status != store.RequestAvailable {
		writeErr(w, http.StatusConflict, "this request was already approved")
		return
	}
	if _, err := s.Store.WithdrawRequest(t.ID, u.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// approveRequest has the request fetched; for a failed one, tried again.
func (s *Server) approveRequest(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	switch t.Status {
	case store.RequestPending, store.RequestDeclined, store.RequestFailed, store.RequestApproved:
	default:
		writeErr(w, http.StatusConflict, "this request was already "+t.Status)
		return
	}
	if err := s.Store.SetRequestStatus(t.ID, store.RequestApproved, "", "", currentUser(r).ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	t, _ = s.Store.TitleRequest(t.ID)
	s.Emit("request.approved", t)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) declineRequest(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if utf8.RuneCountInString(req.Reason) > 500 {
		writeErr(w, http.StatusBadRequest, "keep the reason under 500 characters")
		return
	}
	if t.Status == store.RequestAvailable {
		writeErr(w, http.StatusConflict, "this title is already on the server")
		return
	}
	if err := s.Store.SetRequestStatus(t.ID, store.RequestDeclined, req.Reason, "", currentUser(r).ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	t, _ = s.Store.TitleRequest(t.ID)
	s.Emit("request.declined", t)
	s.notifyRequesters(t, "Request declined: "+yearTitle(t), req.Reason, "/requests", currentUser(r).ID)
	writeJSON(w, http.StatusOK, t)
}

// notifyRequesters tells the people who asked for t (except skip).
func (s *Server) notifyRequesters(t *store.TitleRequest, title, body, link string, skip int64) {
	people, err := s.Store.RequestPeople(t.ID)
	if err != nil {
		return
	}
	for _, uid := range people {
		if uid == skip {
			continue
		}
		n := &store.Notification{UserID: uid, Kind: "request", Title: title, Body: body, Link: link, Image: t.Poster}
		if link == "" {
			// The title is in, maybe in a library they can't see.
			n.Link = "/requests"
			if items, _ := s.Store.FindItems(uid, t.Kind, t.TMDBID, "", 0); len(items) > 0 {
				n.Link = "/item/" + strconv.FormatInt(items[0].ID, 10)
			}
		}
		if err := s.Store.AddNotification(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

// markArrivedRequests closes the requests whose title is now in a library
// and tells the people who asked.
func (s *Server) markArrivedRequests() {
	list, err := s.Store.ArrivedRequests()
	if err != nil {
		slog.Warn("requests", "err", err)
		return
	}
	for _, t := range list {
		if err := s.Store.SetRequestStatus(t.ID, store.RequestAvailable, "", "", 0); err != nil {
			continue
		}
		t.Status = store.RequestAvailable
		s.Emit("request.available", t)
		s.notifyRequesters(t, yearTitle(t)+" is ready to watch", "You asked for it", "", 0)
	}
}

// pluginRequests lists requests for the plugin that fetches them
// (?status=approved for the ones waiting).
func (s *Server) pluginRequests(w http.ResponseWriter, r *http.Request) {
	var statuses []string
	if st := r.URL.Query().Get("status"); st != "" {
		statuses = strings.Split(st, ",")
	}
	list, err := s.Store.TitleRequests(0, statuses...)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// pluginUpdateRequest is the fetching plugin's report: sent (with the
// title's page in the other app) or failed (with why).
func (s *Server) pluginUpdateRequest(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
		Link   string `json:"link"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Status != store.RequestSent && req.Status != store.RequestFailed {
		writeErr(w, http.StatusBadRequest, "status must be sent or failed")
		return
	}
	if req.Link != "" && !strings.HasPrefix(req.Link, "http://") && !strings.HasPrefix(req.Link, "https://") {
		writeErr(w, http.StatusBadRequest, "link must be a web address")
		return
	}
	if len(req.Reason) > 1000 {
		req.Reason = req.Reason[:1000]
	}
	if t.Status != store.RequestApproved && t.Status != store.RequestSent && t.Status != store.RequestFailed {
		// Declined, or already in a library, while the plugin worked.
		writeErr(w, http.StatusConflict, "the request is "+t.Status+" now")
		return
	}
	if err := s.Store.SetRequestStatus(t.ID, req.Status, req.Reason, req.Link, 0); err != nil {
		writeStoreErr(w, err)
		return
	}
	t, _ = s.Store.TitleRequest(t.ID)
	writeJSON(w, http.StatusOK, t)
}
