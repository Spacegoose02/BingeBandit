package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/metadata"
	"bingebandit/internal/store"
)

// Title requests: people ask for a movie or show (or some of its seasons,
// or its 4K copy) the server doesn't have, an admin (or their own rules)
// approves it, and a plugin that lists request.approved in its manifest
// (Cinemask Upgrades, Sonarr & Radarr) fetches it and reports how it's
// going through the plugin API. Once it shows up in a library, the people
// who asked hear about it. Kids' requests go to a parent first.

// requestsOn is the admin's switch for title requests.
func (s *Server) requestsOn() bool {
	return s.Store.Setting(store.SettingRequests) != "false"
}

// canRequest says why u can't ask for titles, or "" when they can.
func (s *Server) canRequest(u *store.User) string {
	switch {
	case !s.requestsOn():
		return "an admin turned requests off"
	case u.Kid && s.Store.Setting(store.SettingRequestKids) != "true":
		return "kids can't request titles"
	}
	return ""
}

func (s *Server) fourKOn() bool { return s.Store.Setting(store.SettingRequest4K) == "true" }

// requestSearchResult is a TMDB title with what the server knows of it.
type requestSearchResult struct {
	metadata.Requestable
	ItemID   int64               `json:"itemId,omitempty"`   // in a library the viewer can see
	OnServer bool                `json:"onServer,omitempty"` // in a library, maybe one the viewer can't see
	Request  *store.TitleRequest `json:"request,omitempty"`
}

func (s *Server) needTMDB(w http.ResponseWriter) bool {
	if s.Metadata == nil || !s.Metadata.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "Requests need a TMDB API key; ask your server admin to add one in Settings")
		return false
	}
	return true
}

// requester checks the viewer may ask for titles, answering 403 when not.
func (s *Server) requester(w http.ResponseWriter, r *http.Request) *store.User {
	u := currentUser(r)
	if why := s.canRequest(u); why != "" {
		writeErr(w, http.StatusForbidden, why)
		return nil
	}
	return u
}

// searchRequestable searches TMDB for movies and shows (or one kind).
func (s *Server) searchRequestable(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil {
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
	if !s.needTMDB(w) {
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
	var list []metadata.Requestable
	for i := 0; i < 20; i++ {
		for k, l := range found {
			if i < len(l) && l[i].TMDBID > 0 {
				it := l[i]
				list = append(list, metadata.Requestable{Kind: kinds[k], TMDBID: it.TMDBID, Title: it.Title, Year: it.Year,
					Overview: it.Overview, Poster: it.Poster})
			}
		}
	}
	writeJSON(w, http.StatusOK, s.annotateRequestable(u, list, false))
}

// annotateRequestable says for each title whether it's on the server and
// who asked for it. missingOnly drops the ones the viewer can already watch.
func (s *Server) annotateRequestable(u *store.User, list []metadata.Requestable, missingOnly bool) []requestSearchResult {
	keys := make([]string, len(list))
	for i, it := range list {
		keys[i] = it.Kind + ":" + strconv.FormatInt(it.TMDBID, 10)
	}
	reqs, _ := s.Store.TitleRequestsFor(keys)
	out := []requestSearchResult{}
	for i, it := range list {
		res := requestSearchResult{Requestable: it}
		if mine, _ := s.Store.FindItems(u.ID, it.Kind, it.TMDBID, "", 0); len(mine) > 0 {
			res.ItemID, res.OnServer = mine[0].ID, true
		} else if all, _ := s.Store.FindItems(0, it.Kind, it.TMDBID, "", 0); len(all) > 0 {
			res.OnServer = true
		}
		if missingOnly && res.OnServer {
			continue
		}
		if t := reqs[keys[i]]; t != nil {
			t.Mine = s.Store.RequestedBy(t.ID, u.ID)
			res.Request = t
		}
		out = append(out, res)
	}
	return out
}

// requestLimit is how many of kind u may ask for in 7 days (0: no limit).
func (s *Server) requestLimit(u *store.User, kind string) int {
	if u.IsAdmin {
		return 0
	}
	rules := s.Store.RequestRules(u.ID)
	n, key := rules.MovieLimit, store.SettingRequestMovieLimit
	if kind == "show" {
		n, key = rules.ShowLimit, store.SettingRequestShowLimit
	}
	if n < 0 {
		n, _ = strconv.Atoi(s.Store.Setting(key))
	}
	return max(n, 0)
}

type limitState struct {
	Limit    int   `json:"limit"` // 0: none
	Used     int   `json:"used"`
	ResetsAt int64 `json:"resetsAt,omitempty"` // when the oldest one counted stops counting
}

const week = 7 * 24 * 3600

func (s *Server) limitState(u *store.User, kind string) limitState {
	l := limitState{Limit: s.requestLimit(u, kind)}
	if l.Limit == 0 {
		return l
	}
	since := time.Now().Unix() - week
	l.Used = s.Store.RequestsSince(u.ID, kind, since)
	if l.Used >= l.Limit {
		l.ResetsAt = s.Store.OldestRequestSince(u.ID, kind, since) + week
	}
	return l
}

// myRequests lists the viewer's requests (with a parent's kids' requests
// waiting for them), or for admins with ?all=1 every request, with whether
// requests are on, the viewer's limits, and who fetches approved ones.
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
	rules := s.Store.RequestRules(u.ID)
	out := map[string]any{
		"requests": list, "enabled": s.requestsOn(), "canRequest": s.canRequest(u) == "", "reason": s.canRequest(u),
		"limits":    map[string]limitState{"movie": s.limitState(u, "movie"), "show": s.limitState(u, "show")},
		"fourK":     s.fourKOn() || u.IsAdmin,
		"watchlist": rules.Watchlist, "trakt": s.Trakt != nil && s.Trakt.Configured(),
		"kidsWait":  u.Kid && u.OwnerID != 0,
		"household": s.kidsWaiting(u),
	}
	if u.IsAdmin {
		handlers := []string{}
		if s.Plugins != nil {
			handlers = s.Plugins.Handlers("request.approved")
		}
		out["handlers"] = handlers
		out["problems"] = s.Store.OpenProblems()
		if days, _ := strconv.Atoi(s.Store.Setting(store.SettingRequestCleanup)); days > 0 {
			out["cleanupDays"] = days
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// isParentOf tells whether u looks after kid: u is a grown-up in the same
// household (the account itself or one of its other profiles).
func isParentOf(u, kid *store.User) bool {
	if u.Kid || !kid.Kid || kid.OwnerID == 0 || u.ID == kid.ID {
		return false
	}
	return kid.OwnerID == u.ID || (u.OwnerID != 0 && u.OwnerID == kid.OwnerID)
}

// kidsWaiting lists the requests u's kids made that wait for a parent.
func (s *Server) kidsWaiting(u *store.User) []*store.TitleRequest {
	out := []*store.TitleRequest{}
	if u.Kid {
		return out
	}
	list, err := s.Store.TitleRequests(0, store.RequestParent)
	if err != nil {
		return out
	}
	for _, t := range list {
		if s.parentFor(u, t) {
			out = append(out, t)
		}
	}
	return out
}

// parentFor tells whether u may answer a kid's request t.
func (s *Server) parentFor(u *store.User, t *store.TitleRequest) bool {
	people, _ := s.Store.RequestPeople(t.ID)
	for _, id := range people {
		if kid, err := s.Store.UserByID(id); err == nil && isParentOf(u, kid) {
			return true
		}
	}
	return false
}

var requestPoster = regexp.MustCompile(`^/api/images/tmdb/([A-Za-z0-9_-]+\.(?:jpe?g|png|webp))$`)

// requestInput is a title someone asks for.
type requestInput struct {
	Kind        string `json:"kind"`
	TMDBID      int64  `json:"tmdbId"`
	Title       string `json:"title"`
	Year        int    `json:"year"`
	Poster      string `json:"poster"`
	Overview    string `json:"overview"`
	Note        string `json:"note"`
	Seasons     []int  `json:"seasons"`
	FourK       bool   `json:"fourK"`
	ReleaseDate string `json:"releaseDate"`
}

// requestError is a request that can't be made, with the status to answer.
type requestError struct {
	code int
	msg  string
}

func (e *requestError) Error() string { return e.msg }

// statusFor is where u's new request starts.
func (s *Server) statusFor(u *store.User) string {
	switch {
	case u.Kid && u.OwnerID != 0:
		return store.RequestParent
	case u.IsAdmin || s.Store.RequestRules(u.ID).AutoApprove:
		return store.RequestApproved
	}
	return store.RequestPending
}

var releaseDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// placeRequest asks for a title for u. source is "" when they asked and
// "watchlist" when their watchlist did (which doesn't count toward limits).
func (s *Server) placeRequest(u *store.User, in requestInput, source string) (*store.TitleRequest, string, error) {
	in.Title, in.Note = strings.TrimSpace(in.Title), strings.TrimSpace(in.Note)
	switch {
	case in.Kind != "movie" && in.Kind != "show":
		return nil, "", &requestError{http.StatusBadRequest, "kind must be movie or show"}
	case in.TMDBID <= 0:
		return nil, "", &requestError{http.StatusBadRequest, "tmdbId is required"}
	case in.Title == "" || utf8.RuneCountInString(in.Title) > 300:
		return nil, "", &requestError{http.StatusBadRequest, "a title is required"}
	case utf8.RuneCountInString(in.Note) > 500:
		return nil, "", &requestError{http.StatusBadRequest, "keep the note under 500 characters"}
	case len(in.Seasons) > 200:
		return nil, "", &requestError{http.StatusBadRequest, "too many seasons"}
	case in.FourK && !s.fourKOn() && !u.IsAdmin:
		return nil, "", &requestError{http.StatusForbidden, "an admin hasn't turned on 4K requests"}
	}
	if in.Year < 0 || in.Year > 3000 {
		in.Year = 0
	}
	if in.Kind == "movie" {
		in.Seasons = nil
	}
	for _, n := range in.Seasons {
		if n <= 0 || n > 500 {
			return nil, "", &requestError{http.StatusBadRequest, "seasons are numbered from 1"}
		}
	}
	if !releaseDate.MatchString(in.ReleaseDate) {
		in.ReleaseDate = ""
	}
	if s.serverCovers(in) {
		return nil, "", &requestError{http.StatusConflict, "this title is already on the server"}
	}
	// Weekly limits count new asks, not joining one they're already on.
	if source == "" {
		if existing := s.Store.TitleRequestFor(in.Kind, in.TMDBID); existing == nil || !s.Store.RequestedBy(existing.ID, u.ID) {
			if l := s.limitState(u, in.Kind); l.Limit > 0 && l.Used >= l.Limit {
				what := "movie"
				if in.Kind == "show" {
					what = "show"
				}
				return nil, "", &requestError{http.StatusTooManyRequests, fmt.Sprintf("You've used your %d %s requests for this week. The next one frees up %s.",
					l.Limit, what, time.Unix(l.ResetsAt, 0).Format("Mon, Jan 2"))}
			}
		}
	}
	poster := ""
	if m := requestPoster.FindStringSubmatch(in.Poster); m != nil {
		poster = "tmdb:/" + m[1]
	}
	if utf8.RuneCountInString(in.Overview) > 2000 {
		in.Overview = string([]rune(in.Overview)[:2000])
	}
	status := s.statusFor(u)
	t, outcome, err := s.Store.RequestTitle(u.ID, &store.TitleRequest{Kind: in.Kind, TMDBID: in.TMDBID, Title: in.Title,
		Year: in.Year, Poster: poster, Overview: in.Overview, Note: in.Note, Seasons: in.Seasons, FourK: in.FourK,
		ReleaseDate: in.ReleaseDate}, status, source)
	if err != nil {
		return nil, "", err
	}
	fresh := outcome != store.RequestJoined
	switch {
	case fresh && status == store.RequestApproved:
		s.Store.SetRequestStatus(t.ID, status, "", "", u.ID)
		t.Status = status
		s.Emit("request.approved", t)
	case fresh && status == store.RequestParent:
		s.notifyParents(t, u)
	case fresh:
		s.Emit("request.created", t)
		s.notifyAdminsOfRequest(t, u)
	case t.Status == store.RequestParent && status != store.RequestParent:
		// A grown-up asked for something a kid is waiting on.
		s.moveOn(t, status, u)
	case status == store.RequestApproved && t.Status == store.RequestPending:
		// Someone who's approved at once asking for something already asked for approves it.
		if err := s.Store.SetRequestStatus(t.ID, store.RequestApproved, "", "", u.ID); err == nil {
			t.Status = store.RequestApproved
			s.Emit("request.approved", t)
		}
	}
	t.Mine = true
	return t, outcome, nil
}

// moveOn takes a request from waiting on a parent to pending (telling the
// admins) or approved, as status says.
func (s *Server) moveOn(t *store.TitleRequest, status string, by *store.User) {
	if err := s.Store.SetRequestStatus(t.ID, status, "", "", by.ID); err != nil {
		return
	}
	t.Status = status
	if status == store.RequestApproved {
		s.Emit("request.approved", t)
		return
	}
	s.Emit("request.created", t)
	s.notifyAdminsOfRequest(t, by)
}

// serverCovers tells whether the server already has what in asks for.
func (s *Server) serverCovers(in requestInput) bool {
	found, _ := s.Store.FindItems(0, in.Kind, in.TMDBID, "", 0)
	if len(found) == 0 {
		return false
	}
	if in.FourK && !s.Store.Has4K(in.Kind, in.TMDBID) {
		return false
	}
	if in.Kind == "show" && len(in.Seasons) > 0 {
		have, err := s.Store.ShowSeasons(in.TMDBID)
		if err != nil {
			return false // can't tell; let the request through
		}
		for _, n := range in.Seasons {
			if have[n] == 0 {
				return false
			}
		}
	}
	return true
}

// createRequest asks for a title.
func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil {
		return
	}
	var in requestInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	t, outcome, err := s.placeRequest(u, in, "")
	var re *requestError
	if errors.As(err, &re) {
		writeErr(w, re.code, re.msg)
		return
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	code := http.StatusOK
	if outcome == store.RequestCreated {
		code = http.StatusCreated
	}
	writeJSON(w, code, t)
}

// notifyAdminsOfRequest tells the admins (not the one asking) about a new request.
func (s *Server) notifyAdminsOfRequest(t *store.TitleRequest, from *store.User) {
	users, err := s.Store.ListUsers()
	if err != nil {
		return
	}
	for _, a := range users {
		if !a.IsAdmin || a.OwnerID != 0 || a.ID == from.ID {
			continue
		}
		n := &store.Notification{UserID: a.ID, Kind: "request", Title: personName(from) + " requested " + requestName(t),
			Body: t.Note, Link: "/requests", Image: t.Poster}
		if err := s.notify(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

// notifyParents tells a kid's household account about the kid's request.
func (s *Server) notifyParents(t *store.TitleRequest, kid *store.User) {
	n := &store.Notification{UserID: kid.OwnerID, Kind: "request", Title: personName(kid) + " would like " + requestName(t),
		Body: "Say yes or no on the Requests page.", Link: "/requests", Image: t.Poster}
	if err := s.notify(n); err != nil {
		slog.Warn("notification", "err", err)
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

// requestName is the title with the seasons asked for: "Severance
// (2022), seasons 1–2".
func requestName(t *store.TitleRequest) string {
	name := yearTitle(t)
	if len(t.Seasons) > 0 {
		name += ", " + seasonList(t.Seasons)
	}
	if t.FourK {
		name += " in 4K"
	}
	return name
}

func seasonList(seasons []int) string {
	if len(seasons) == 1 {
		return "season " + strconv.Itoa(seasons[0])
	}
	parts := make([]string, len(seasons))
	for i, n := range seasons {
		parts[i] = strconv.Itoa(n)
	}
	return "seasons " + strings.Join(parts, ", ")
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
	switch t.Status {
	case store.RequestPending, store.RequestParent, store.RequestDeclined, store.RequestAvailable:
	default:
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
	case store.RequestPending, store.RequestParent, store.RequestDeclined, store.RequestFailed, store.RequestApproved, store.RequestSent:
	default:
		writeErr(w, http.StatusConflict, "this request was already "+t.Status)
		return
	}
	u := currentUser(r)
	was := t.Status
	if err := s.Store.SetRequestStatus(t.ID, store.RequestApproved, "", "", u.ID); err != nil {
		writeStoreErr(w, err)
		return
	}
	t, _ = s.Store.TitleRequest(t.ID)
	s.Emit("request.approved", t)
	if was == store.RequestPending || was == store.RequestParent || was == store.RequestDeclined {
		s.notifyRequesters(t, "Request approved: "+requestName(t), "It's on its way. You'll hear when it's ready.", "/requests", u.ID)
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) declineRequest(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	s.decline(w, r, t)
}

func (s *Server) decline(w http.ResponseWriter, r *http.Request, t *store.TitleRequest) {
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
	s.notifyRequesters(t, "Request declined: "+requestName(t), req.Reason, "/requests", currentUser(r).ID)
	writeJSON(w, http.StatusOK, t)
}

// parentAnswer is a parent saying yes or no to their kid's request. Yes
// sends it on as if the parent had asked: to the admins, or straight to
// approved when the parent's requests are.
func (s *Server) parentAnswer(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	u := currentUser(r)
	if t.Status != store.RequestParent || !s.parentFor(u, t) {
		writeErr(w, http.StatusNotFound, "no request of your kids' is waiting with that id")
		return
	}
	if r.URL.Query().Get("answer") == "no" {
		s.decline(w, r, t)
		return
	}
	status := store.RequestPending
	if u.IsAdmin || s.Store.RequestRules(u.ID).AutoApprove {
		status = store.RequestApproved
	}
	s.moveOn(t, status, u)
	t, _ = s.Store.TitleRequest(t.ID)
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
		if err := s.notify(n); err != nil {
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
		s.notifyRequesters(t, requestName(t)+" is ready to watch", "You asked for it. Tap to play.", "", 0)
	}
}

// titleInfo is a movie or show from TMDB with what the server has of it:
// for a show, each season's episodes here, so people can ask for the rest.
func (s *Server) titleInfo(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil || !s.needTMDB(w) {
		return
	}
	kind := chi.URLParam(r, "kind")
	id, _ := strconv.ParseInt(chi.URLParam(r, "tmdb"), 10, 64)
	if (kind != "movie" && kind != "show") || id <= 0 {
		writeErr(w, http.StatusBadRequest, "bad title")
		return
	}
	info, err := s.Metadata.TitleInfo(r.Context(), kind, id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	type season struct {
		metadata.SeasonInfo
		OnServer  int  `json:"onServer"` // episodes the server has
		Requested bool `json:"requested"`
	}
	ann := s.annotateRequestable(u, []metadata.Requestable{info.Requestable}, false)[0]
	out := map[string]any{"title": ann, "backdrop": info.Backdrop, "status": info.Status, "has4K": s.Store.Has4K(kind, id),
		"fourK": s.fourKOn() || u.IsAdmin, "limit": s.limitState(u, kind), "kidsWait": u.Kid && u.OwnerID != 0}
	if kind == "show" {
		have, err := s.Store.ShowSeasons(id)
		if err != nil {
			// Without the counts every season would look missing; leave the
			// seasons out so the page doesn't offer to request what's here.
			slog.Warn("requests: show seasons", "tmdb", id, "err", err)
			writeJSON(w, http.StatusOK, out)
			return
		}
		asked := map[int]bool{}
		whole := false
		if ann.Request != nil && ann.Request.Status != store.RequestDeclined {
			whole = len(ann.Request.Seasons) == 0
			for _, n := range ann.Request.Seasons {
				asked[n] = true
			}
		}
		seasons := []season{}
		for _, se := range info.Seasons {
			seasons = append(seasons, season{SeasonInfo: se, OnServer: have[se.Number], Requested: whole || asked[se.Number]})
		}
		out["seasons"] = seasons
	}
	writeJSON(w, http.StatusOK, out)
}

// discoverRow is one row of the Requests page.
type discoverRow struct {
	Key   string                `json:"key"`
	Title string                `json:"title"`
	Items []requestSearchResult `json:"items"`
}

// discoverRequests is the Requests page's rows: titles like the ones the
// viewer finished lately, what's trending, and what's coming soon, leaving
// out what the server already has.
func (s *Server) discoverRequests(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil || !s.needTMDB(w) {
		return
	}
	type source struct {
		key, title string
		fetch      func(ctx context.Context) ([]metadata.Requestable, error)
	}
	var sources []source
	for i, f := range s.Store.RecentlyFinished(u.ID, 2) {
		sources = append(sources, source{fmt.Sprintf("because-%d", i), "Because you watched " + f.Title,
			func(ctx context.Context) ([]metadata.Requestable, error) {
				return s.Metadata.Recommendations(ctx, f.Kind, f.TMDBID)
			}})
	}
	for _, l := range []struct{ key, title string }{
		{"trending", "Trending this week"},
		{"upcoming-movies", "Coming soon to theaters"},
		{"upcoming-shows", "New episodes on TV"},
		{"popular-movies", "Popular movies"},
		{"popular-shows", "Popular shows"},
	} {
		sources = append(sources, source{l.key, l.title,
			func(ctx context.Context) ([]metadata.Requestable, error) { return s.Metadata.Discover(ctx, l.key) }})
	}
	rows := make([]discoverRow, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Go(func() {
			list, err := src.fetch(r.Context())
			if err != nil {
				slog.Debug("discover requests", "row", src.key, "err", err)
				return
			}
			rows[i] = discoverRow{Key: src.key, Title: src.title, Items: s.annotateRequestable(u, list, true)}
		})
	}
	wg.Wait()
	out := []discoverRow{}
	for _, row := range rows {
		if len(row.Items) > 0 {
			out = append(out, row)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// personRequests lists a cast or crew member's movies and shows the
// server doesn't have.
func (s *Server) personRequests(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil || !s.needTMDB(w) {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	list, err := s.Metadata.PersonCredits(r.Context(), name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.annotateRequestable(u, list, true))
}

// similarRequests lists titles like a movie or show the server doesn't have.
func (s *Server) similarRequests(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil || !s.needTMDB(w) {
		return
	}
	kind := chi.URLParam(r, "kind")
	id, _ := strconv.ParseInt(chi.URLParam(r, "tmdb"), 10, 64)
	if (kind != "movie" && kind != "show") || id <= 0 {
		writeErr(w, http.StatusBadRequest, "bad title")
		return
	}
	list, err := s.Metadata.Recommendations(r.Context(), kind, id)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.annotateRequestable(u, list, true))
}

// collectionRequests lists the titles of a collection's TMDB collection or
// lists that no library has, so people can ask for them.
func (s *Server) collectionRequests(w http.ResponseWriter, r *http.Request) {
	u := s.requester(w, r)
	if u == nil {
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	c, err := s.Store.Collection(id)
	if err != nil || !s.Store.CanSeeLibrary(u, c.LibraryID) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	missing, _, err := s.collectionMissing(r.Context(), c)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	list := make([]metadata.Requestable, 0, len(missing))
	for _, m := range missing {
		list = append(list, metadata.Requestable{Kind: m.Kind, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year})
	}
	out := s.annotateRequestable(u, list, true)
	// Lists don't carry posters; TMDB has them (cached), for the first few.
	if s.Metadata != nil && s.Metadata.Enabled() {
		var wg sync.WaitGroup
		for i := range out[:min(len(out), 40)] {
			wg.Go(func() {
				if info, err := s.Metadata.TitleInfo(r.Context(), out[i].Kind, out[i].TMDBID); err == nil {
					out[i].Poster, out[i].Overview, out[i].ReleaseDate = info.Poster, info.Overview, info.ReleaseDate
				}
			})
		}
		wg.Wait()
	}
	writeJSON(w, http.StatusOK, out)
}

// requestSettings are the viewer's own request choices: whether their
// Trakt watchlist is requested for them.
func (s *Server) requestSettings(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]bool{"watchlist": s.Store.RequestRules(u.ID).Watchlist})
		return
	}
	if s.requester(w, r) == nil {
		return
	}
	var in struct {
		Watchlist bool `json:"watchlist"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Watchlist {
		if s.Trakt == nil || !s.Trakt.Configured() {
			writeErr(w, http.StatusBadRequest, "an admin needs to set up Trakt first")
			return
		}
		if _, err := s.Store.TraktAccount(u.ID); err != nil {
			writeErr(w, http.StatusBadRequest, "connect Trakt under Account first")
			return
		}
	}
	rules := s.Store.RequestRules(u.ID)
	rules.Watchlist = in.Watchlist
	if err := s.Store.SetRequestRules(u.ID, rules); err != nil {
		writeStoreErr(w, err)
		return
	}
	if in.Watchlist {
		go s.requestWatchlist(context.Background(), u.ID)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"watchlist": in.Watchlist})
}

// adminRequestRules lists each person's request rules with the server's.
func (s *Server) adminRequestRules(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	all := s.Store.AllRequestRules()
	type row struct {
		ID      int64              `json:"id"`
		Name    string             `json:"name"`
		Admin   bool               `json:"isAdmin"`
		Kid     bool               `json:"kid"`
		Profile bool               `json:"profile"`
		Rules   store.RequestRules `json:"rules"`
		Movies  limitState         `json:"movies"`
		Shows   limitState         `json:"shows"`
	}
	out := []row{}
	for _, u := range users {
		rules, ok := all[u.ID]
		if !ok {
			rules = store.RequestRules{MovieLimit: -1, ShowLimit: -1}
		}
		out = append(out, row{u.ID, personName(u), u.IsAdmin, u.Kid, u.OwnerID != 0, rules, s.limitState(u, "movie"), s.limitState(u, "show")})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setAdminRequestRules(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, err := s.Store.UserByID(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	var in struct {
		AutoApprove bool `json:"autoApprove"`
		MovieLimit  int  `json:"movieLimit"`
		ShowLimit   int  `json:"showLimit"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	rules := s.Store.RequestRules(id)
	rules.AutoApprove = in.AutoApprove
	rules.MovieLimit = min(max(in.MovieLimit, -1), 1000)
	rules.ShowLimit = min(max(in.ShowLimit, -1), 1000)
	if err := s.Store.SetRequestRules(id, rules); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

// unwatchedRequests lists requested titles that came in a while ago and
// that nobody who asked for them has played, so an admin can free the space.
func (s *Server) unwatchedRequests(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(s.Store.Setting(store.SettingRequestCleanup))
	if q, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && q > 0 {
		days = q
	}
	if days <= 0 {
		writeJSON(w, http.StatusOK, map[string]any{"days": 0, "requests": []any{}})
		return
	}
	list, err := s.Store.UnwatchedRequests(days)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type row struct {
		*store.TitleRequest
		Items []int64 `json:"items"`
	}
	out := []row{}
	for _, t := range list {
		out = append(out, row{t, s.Store.ItemsForRequest(t)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "requests": out})
}

// pluginRequests lists requests for the plugin that fetches them
// (?status=approved for the ones waiting), the most wanted first.
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
	sort.SliceStable(list, func(i, j int) bool { return list[i].People > list[j].People })
	writeJSON(w, http.StatusOK, list)
}

// pluginUpdateRequest is the fetching plugin's report: sent (with the
// title's page in the other app, and how far along it is) or failed (with why).
func (s *Server) pluginUpdateRequest(w http.ResponseWriter, r *http.Request) {
	t := s.requestParam(w, r)
	if t == nil {
		return
	}
	var req struct {
		Status   string   `json:"status"`
		Reason   string   `json:"reason"`
		Link     string   `json:"link"`
		Stage    string   `json:"stage"`
		Progress *float64 `json:"progress"`
		ETA      int64    `json:"eta"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Status != store.RequestSent && req.Status != store.RequestFailed {
		writeErr(w, http.StatusBadRequest, "status must be sent or failed")
		return
	}
	switch req.Stage {
	case "", "searching", "downloading", "importing", "waiting":
	default:
		writeErr(w, http.StatusBadRequest, "stage must be searching, downloading, importing or waiting")
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
	if t.Status != req.Status || req.Reason != t.Reason || (req.Link != "" && req.Link != t.Link) {
		if err := s.Store.SetRequestStatus(t.ID, req.Status, req.Reason, req.Link, 0); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	if req.Status == store.RequestSent && (req.Stage != "" || req.Progress != nil) {
		p := 0.0
		if req.Progress != nil {
			p = *req.Progress
		}
		if err := s.Store.SetRequestProgress(t.ID, req.Stage, p, req.ETA); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	t, _ = s.Store.TitleRequest(t.ID)
	writeJSON(w, http.StatusOK, t)
}

// offerNextSeason runs when u finishes an episode: when it was the last one
// the server has and TMDB knows of a later season, u gets a notification
// offering to request it (once per season).
func (s *Server) offerNextSeason(u *store.User, ep *store.Item) {
	if ep.Kind != "episode" || ep.ShowID == 0 || ep.SeasonNumber == nil || s.canRequest(u) != "" || s.Metadata == nil || !s.Metadata.Enabled() {
		return
	}
	show, err := s.Store.Item(u.ID, ep.ShowID)
	if err != nil || show.TMDBID == 0 {
		return
	}
	have, err := s.Store.ShowSeasons(show.TMDBID)
	if err != nil {
		return
	}
	last := 0
	for n := range have {
		last = max(last, n)
	}
	if *ep.SeasonNumber != last {
		return
	}
	eps, _ := s.Store.ShowEpisodes(u.ID, show.ID)
	for _, e := range eps {
		if e.SeasonNumber != nil && *e.SeasonNumber == last && e.EpisodeNumber != nil && ep.EpisodeNumber != nil && *e.EpisodeNumber > *ep.EpisodeNumber {
			return // more to watch here first
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := s.Metadata.TitleInfo(ctx, "show", show.TMDBID)
	if err != nil {
		return
	}
	today := time.Now().Format("2006-01-02")
	for _, se := range info.Seasons {
		if se.Number <= last || have[se.Number] > 0 {
			continue
		}
		if t := s.Store.TitleRequestFor("show", show.TMDBID); t != nil && t.Status != store.RequestDeclined &&
			(len(t.Seasons) == 0 || slicesContains(t.Seasons, se.Number)) {
			return // already asked for
		}
		if !s.Store.OfferSeason(u.ID, show.TMDBID, se.Number) {
			return
		}
		title, body := fmt.Sprintf("You're caught up on %s", show.Title), fmt.Sprintf("Season %d is out. Want it here?", se.Number)
		if se.AirDate == "" || se.AirDate > today {
			when := "soon"
			if d, err := time.Parse("2006-01-02", se.AirDate); err == nil {
				when = "on " + d.Format("Jan 2")
			}
			body = fmt.Sprintf("Season %d starts %s. Request it to have it here on day one.", se.Number, when)
		}
		n := &store.Notification{UserID: u.ID, Kind: "request", Title: title, Body: body,
			Link: fmt.Sprintf("/requests?title=show:%d&season=%d", show.TMDBID, se.Number), Image: store.ImageURL(show.Poster)}
		if err := s.notify(n); err != nil {
			slog.Warn("notification", "err", err)
		}
		return
	}
}

func slicesContains(list []int, n int) bool {
	for _, v := range list {
		if v == n {
			return true
		}
	}
	return false
}

// requestWatchlists asks for what's on the Trakt watchlists of the people
// who turned that on (every 6 hours, with the notifications).
func (s *Server) requestWatchlists(ctx context.Context) {
	for _, uid := range s.Store.WatchlistRequesters() {
		if ctx.Err() != nil {
			return
		}
		s.requestWatchlist(ctx, uid)
	}
}

// watchlistRun keeps one person's watchlist from being read twice at once.
var watchlistRun sync.Map

func (s *Server) requestWatchlist(ctx context.Context, uid int64) {
	if _, busy := watchlistRun.LoadOrStore(uid, true); busy {
		return
	}
	defer watchlistRun.Delete(uid)
	if s.Trakt == nil || !s.requestsOn() {
		return
	}
	u, err := s.Store.UserByID(uid)
	if err != nil || s.canRequest(u) != "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	list, err := s.Trakt.Watchlist(ctx, uid)
	if err != nil {
		slog.Info("watchlist requests", "user", uid, "err", err)
		return
	}
	added := 0
	for _, wt := range list {
		if added >= 25 || ctx.Err() != nil {
			return
		}
		if found, _ := s.Store.FindItems(0, wt.Kind, wt.TMDBID, "", 0); len(found) > 0 {
			continue
		}
		if t := s.Store.TitleRequestFor(wt.Kind, wt.TMDBID); t != nil && s.Store.RequestedBy(t.ID, uid) {
			continue
		}
		in := requestInput{Kind: wt.Kind, TMDBID: wt.TMDBID, Title: wt.Title, Year: wt.Year, Note: "From their Trakt watchlist"}
		if s.Metadata != nil && s.Metadata.Enabled() {
			if info, err := s.Metadata.TitleInfo(ctx, wt.Kind, wt.TMDBID); err == nil {
				in.Title, in.Year, in.Poster, in.Overview, in.ReleaseDate = info.Title, info.Year, info.Poster, info.Overview, info.ReleaseDate
			}
		}
		if _, outcome, err := s.placeRequest(u, in, "watchlist"); err == nil && outcome != store.RequestJoined {
			added++
		}
	}
}

// filmographyCredit is a title on a person's filmography with what the
// server has of it.
type filmographyCredit struct {
	requestSearchResult
	Crew     bool `json:"crew,omitempty"`
	Episodes int  `json:"episodes,omitempty"`
}

// personFilmography is everything a cast or crew member was in, from TMDB,
// marked with what the viewer can watch here and what they could ask for.
// Anyone can see it; the request parts are only filled in for people who
// may request titles.
func (s *Server) personFilmography(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	if s.Metadata == nil || !s.Metadata.Enabled() {
		writeErr(w, http.StatusServiceUnavailable, "a filmography needs a TMDB API key")
		return
	}
	f, err := s.Metadata.PersonFilmography(r.Context(), name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if f == nil {
		writeErr(w, http.StatusNotFound, "TMDB doesn't know "+name)
		return
	}
	list := make([]metadata.Requestable, len(f.Credits))
	for i, c := range f.Credits {
		list[i] = c.Requestable
	}
	why := s.canRequest(u)
	marked := s.annotateRequestable(u, list, false)
	credits := make([]filmographyCredit, len(marked))
	for i, m := range marked {
		if why != "" {
			// Nothing to ask for, and no hints about libraries they can't see.
			m.OnServer, m.Request = m.ItemID != 0, nil
		}
		credits[i] = filmographyCredit{requestSearchResult: m, Crew: f.Credits[i].Crew, Episodes: f.Credits[i].Episodes}
	}
	person := *f
	person.Credits = nil
	writeJSON(w, http.StatusOK, struct {
		metadata.Filmography
		Credits    []filmographyCredit `json:"credits"`
		CanRequest bool                `json:"canRequest"`
		Why        string              `json:"whyNoRequests,omitempty"`
	}{person, credits, why == "", why})
}
