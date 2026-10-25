package api

import (
	"fmt"
	"net/http"
	"os"

	"bingebandit/internal/store"
)

// canRecord: admins always; others when the admin allows users to record.
func (s *Server) canRecord(u *store.User) bool {
	return u.IsAdmin || s.Store.Setting(store.SettingDVRAllowUsers) == "true"
}

func (s *Server) needsApproval(u *store.User) bool {
	return !u.IsAdmin && s.Store.Setting(store.SettingDVRApproval) == "true"
}

// scheduleRecording records a guide program, optionally every airing of its title.
func (s *Server) scheduleRecording(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !s.canRecord(u) {
		writeErr(w, http.StatusForbidden, "recording is turned off for your account")
		return
	}
	var req struct {
		ChannelID int64 `json:"channelId"`
		ProgramID int64 `json:"programId"`
		Series    bool  `json:"series"`
		KeepLast  int   `json:"keepLast"` // series only: keep the newest N recordings (0 = all)
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ch, err := s.Store.Channel(u.ID, req.ChannelID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	p, err := s.Store.ProgramByID(req.ProgramID)
	if err == nil && p.EPGID != ch.EPGID {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if req.KeepLast < 0 || req.KeepLast > maxKeepLast {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("keepLast must be from 0 to %d", maxKeepLast))
		return
	}
	rec, err := s.DVR.Schedule(p, ch, u.ID, req.Series, req.KeepLast, s.needsApproval(u))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if conflicts, err := s.DVR.Conflicts(); err == nil {
		rec.Conflict = conflicts[rec.ID]
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) recordings(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	uid := u.ID
	if u.IsAdmin {
		uid = 0
	}
	recs, err := s.Store.Recordings(uid)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	rules, err := s.Store.Rules(uid, "")
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	conflicts, _ := s.DVR.Conflicts() // across everyone's recordings: they share the providers
	for _, rec := range recs {
		rec.Conflict = conflicts[rec.ID]
		if !u.IsAdmin {
			rec.Path = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recordings": recs, "rules": rules, "folder": s.DVR.Folder(), "canRecord": s.canRecord(u),
	})
}

// ownRecording loads {id} if it's the user's own, or any for admins.
func (s *Server) ownRecording(w http.ResponseWriter, r *http.Request) *store.Recording {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return nil
	}
	rec, err := s.Store.Recording(id)
	if err == nil && rec.UserID != currentUser(r).ID && !currentUser(r).IsAdmin {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return nil
	}
	return rec
}

// deleteRecording cancels an upcoming recording, stops one in progress, or
// removes a finished one (and its file with ?file=1).
func (s *Server) deleteRecording(w http.ResponseWriter, r *http.Request) {
	rec := s.ownRecording(w, r)
	if rec == nil {
		return
	}
	switch rec.Status {
	case "recording":
		s.DVR.Cancel(rec.ID) // keeps the partial file and marks it finished
	case "pending", "scheduled":
		if err := s.Store.DeleteRecording(rec.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
	default:
		if r.URL.Query().Get("file") == "1" && rec.Path != "" && s.DVR.InFolder(rec.Path) {
			if err := os.Remove(rec.Path); err != nil && !os.IsNotExist(err) {
				writeErr(w, http.StatusInternalServerError, "could not delete the file: "+err.Error())
				return
			}
		}
		if err := s.Store.DeleteRecording(rec.ID); err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	rule, err := s.Store.Rule(id)
	if err == nil && rule.UserID != currentUser(r).ID && !currentUser(r).IsAdmin {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.Store.DeleteRule(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const maxKeepLast = 100

// updateRule changes a series rule's keep-last count and prunes right away.
func (s *Server) updateRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	rule, err := s.Store.Rule(id)
	if err == nil && rule.UserID != currentUser(r).ID && !currentUser(r).IsAdmin {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	var req struct {
		KeepLast *int `json:"keepLast"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.KeepLast != nil {
		if *req.KeepLast < 0 || *req.KeepLast > maxKeepLast {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("keepLast must be from 0 to %d", maxKeepLast))
			return
		}
		if err := s.Store.SetRuleKeep(id, *req.KeepLast); err != nil {
			writeStoreErr(w, err)
			return
		}
		s.DVR.Prune(id)
	}
	rule, err = s.Store.Rule(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) approveRecording(w http.ResponseWriter, r *http.Request) {
	rec := s.ownRecording(w, r)
	if rec == nil {
		return
	}
	if rec.Status != "pending" {
		writeErr(w, http.StatusConflict, "only pending recordings can be approved")
		return
	}
	if err := s.Store.SetRecordingStatus(rec.ID, "scheduled"); err != nil {
		writeStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.SetRuleStatus(id, "active"); err != nil {
		writeStoreErr(w, err)
		return
	}
	// Recordings already queued for this rule become scheduled too.
	recs, _ := s.Store.Recordings(0)
	for _, rec := range recs {
		if rec.RuleID == id && rec.Status == "pending" {
			s.Store.SetRecordingStatus(rec.ID, "scheduled")
		}
	}
	go s.DVR.ApplyRules()
	w.WriteHeader(http.StatusNoContent)
}
