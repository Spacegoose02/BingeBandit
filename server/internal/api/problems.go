package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"bingebandit/internal/importer"
	"bingebandit/internal/store"
)

// Problem reports: anyone can say, from the player or a title's page, that
// a movie or episode has bad picture or sound, missing subtitles or is the
// wrong file. Admins see them on the Requests page and either answer them
// or have the fetching plugin get a new copy (the problem.refetch event,
// which carries the report with the title's TMDB id, season and episode).

var problemNames = map[string]string{
	"video":     "picture problem",
	"audio":     "sound problem",
	"subtitles": "subtitle problem",
	"wrong":     "wrong file",
	"other":     "problem",
}

func (s *Server) collectionMissing(ctx context.Context, c *store.Collection) ([]importer.Wanted, int, error) {
	return importer.NewLists(s.Store).Missing(ctx, c)
}

// reportProblem is POST /api/items/{id}/problems.
func (s *Server) reportProblem(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	it, err := s.Store.Item(u.ID, id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if it.Kind != "movie" && it.Kind != "episode" {
		writeErr(w, http.StatusBadRequest, "report a problem with a movie or an episode")
		return
	}
	var in struct {
		Kind     string  `json:"kind"`
		Note     string  `json:"note"`
		Position float64 `json:"position"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	switch {
	case !slices.Contains(store.ProblemKinds, in.Kind):
		writeErr(w, http.StatusBadRequest, "kind must be one of "+strings.Join(store.ProblemKinds, ", "))
		return
	case utf8.RuneCountInString(in.Note) > 1000:
		writeErr(w, http.StatusBadRequest, "keep the note under 1000 characters")
		return
	}
	p, created, err := s.Store.ReportProblem(&store.ProblemReport{ItemID: it.ID, UserID: u.ID, Kind: in.Kind, Note: in.Note, Position: max(in.Position, 0)})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if created {
		s.Emit("problem.created", p)
		s.notifyAdminsOfProblem(p, u)
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, p)
}

func problemTitle(p *store.ProblemReport) string {
	if p.ItemKind == "episode" && p.Season != nil && p.Episode != nil {
		return fmt.Sprintf("%s S%02dE%02d", p.ShowTitle, *p.Season, *p.Episode)
	}
	return p.Title
}

func (s *Server) notifyAdminsOfProblem(p *store.ProblemReport, from *store.User) {
	users, err := s.Store.ListUsers()
	if err != nil {
		return
	}
	for _, a := range users {
		if !a.IsAdmin || a.OwnerID != 0 || a.ID == from.ID {
			continue
		}
		n := &store.Notification{UserID: a.ID, Kind: "request", Title: fmt.Sprintf("%s reported a %s: %s", personName(from), problemNames[p.Kind], problemTitle(p)),
			Body: p.Note, Link: "/requests?tab=problems", Image: p.Poster}
		if err := s.notify(n); err != nil {
			slog.Warn("notification", "err", err)
		}
	}
}

// problems lists the viewer's reports, or for admins with ?all=1
// everyone's (with resolved ones when ?resolved=1).
func (s *Server) problems(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	uid := u.ID
	if u.IsAdmin && r.URL.Query().Get("all") == "1" {
		uid = 0
	}
	list, err := s.Store.ProblemReports(uid, uid != 0 || r.URL.Query().Get("resolved") == "1")
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// answerProblem is an admin's answer: resolve it (with a reply), reopen
// it, or have the fetching plugin get a new copy (refetch).
func (s *Server) answerProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	p, err := s.Store.ProblemReport(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var in struct {
		Action string `json:"action"` // resolve | reopen | refetch
		Reply  string `json:"reply"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	in.Reply = strings.TrimSpace(in.Reply)
	if utf8.RuneCountInString(in.Reply) > 1000 {
		writeErr(w, http.StatusBadRequest, "keep the reply under 1000 characters")
		return
	}
	status := map[string]string{"resolve": store.ProblemResolved, "reopen": store.ProblemOpen, "refetch": store.ProblemFixing}[in.Action]
	if status == "" {
		writeErr(w, http.StatusBadRequest, "action must be resolve, reopen or refetch")
		return
	}
	if in.Action == "refetch" && (s.Plugins == nil || len(s.Plugins.Handlers("problem.refetch")) == 0) {
		writeErr(w, http.StatusConflict, "nothing can fetch a new copy: turn on the Cinemask Upgrades or Sonarr & Radarr plugin")
		return
	}
	if err := s.Store.SetProblemStatus(p.ID, status, in.Reply); err != nil {
		writeStoreErr(w, err)
		return
	}
	p, _ = s.Store.ProblemReport(p.ID)
	switch in.Action {
	case "refetch":
		s.Emit("problem.refetch", p)
		s.notifyReporter(p, "Getting a new copy of "+problemTitle(p), in.Reply)
	case "resolve":
		s.notifyReporter(p, "Fixed: "+problemTitle(p), in.Reply)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) notifyReporter(p *store.ProblemReport, title, body string) {
	n := &store.Notification{UserID: p.UserID, Kind: "request", Title: title, Body: body,
		Link: "/item/" + strconv.FormatInt(p.ItemID, 10), Image: p.Poster}
	if err := s.notify(n); err != nil {
		slog.Warn("notification", "err", err)
	}
}

func (s *Server) deleteProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	u := currentUser(r)
	p, err := s.Store.ProblemReport(id)
	if err != nil || (!u.IsAdmin && p.UserID != u.ID) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Store.DeleteProblemReport(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pluginProblems lists reports for the fetching plugin (?status=fixing).
func (s *Server) pluginProblems(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ProblemReports(0, true)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if st := r.URL.Query().Get("status"); st != "" {
		keep := strings.Split(st, ",")
		list = slices.DeleteFunc(list, func(p *store.ProblemReport) bool { return !slices.Contains(keep, p.Status) })
	}
	writeJSON(w, http.StatusOK, list)
}

// pluginUpdateProblem is the plugin's report on a refetch: resolved (a new
// copy is on its way or in) or open again (it couldn't, with why).
func (s *Server) pluginUpdateProblem(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	p, err := s.Store.ProblemReport(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var in struct {
		Status string `json:"status"`
		Reply  string `json:"reply"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Status != store.ProblemResolved && in.Status != store.ProblemOpen && in.Status != store.ProblemFixing {
		writeErr(w, http.StatusBadRequest, "status must be open, fixing or resolved")
		return
	}
	if len(in.Reply) > 1000 {
		in.Reply = in.Reply[:1000]
	}
	if err := s.Store.SetProblemStatus(p.ID, in.Status, in.Reply); err != nil {
		writeStoreErr(w, err)
		return
	}
	p, _ = s.Store.ProblemReport(p.ID)
	writeJSON(w, http.StatusOK, p)
}
