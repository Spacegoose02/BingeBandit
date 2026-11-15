package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Problem reports: each one someone makes in BingeBandit is filed as a
// report in Cinemask ("Report a problem"), about the movie, the episode or
// (when they said so) the episode's whole season, on problem.created and
// every 2 minutes for ones made while the plugin was off. Cinemask's state
// comes back every 2 minutes (except that a report fixed in BingeBandit
// stays fixed), so a report Cinemask is fixing shows as
// "Getting a new copy" in BingeBandit and is marked fixed (and the person
// told) once Cinemask has the new file. When an admin asks for a new copy
// (problem.refetch), Cinemask's own fix runs: it blocklists the copy
// that's there and grabs another, or fetches subtitles for a subtitle
// report. Fixing or reopening a report in BingeBandit does the same in
// Cinemask.

// problem is a problem report, as the plugin API lists it.
type problem struct {
	ID          int64   `json:"id"`
	Kind        string  `json:"kind"`
	ItemKind    string  `json:"itemKind"`
	TMDBID      int64   `json:"tmdbId"`
	Title       string  `json:"title"`
	Show        string  `json:"showTitle"`
	Season      *int    `json:"seasonNumber"`
	Episode     *int    `json:"episodeNumber"`
	WholeSeason bool    `json:"wholeSeason"`
	User        string  `json:"user"`
	Note        string  `json:"note"`
	Position    float64 `json:"position"`
	Status      string  `json:"status"`
	Reply       string  `json:"reply"`
	RemoteID    string  `json:"remoteId"`
}

// Issue is one of Cinemask's problem reports.
type Issue struct {
	ID         int64   `json:"id"`
	Status     string  `json:"status"` // open, fixing, resolved
	Resolution *string `json:"resolution"`
}

func (i *Issue) resolution() string {
	if i.Resolution == nil {
		return ""
	}
	return *i.Resolution
}

// FileIssue reports a problem to Cinemask. The same problem already open
// there comes back instead of a new one.
func (c *Cinemask) FileIssue(ctx context.Context, body map[string]any) (*Issue, error) {
	var out Issue
	if err := c.do(ctx, "POST", "/api/issues", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Issues lists Cinemask's reports, most recently changed first (300 at most).
func (c *Cinemask) Issues(ctx context.Context) ([]Issue, error) {
	var out []Issue
	return out, c.do(ctx, "GET", "/api/issues?status=all", nil, &out)
}

// FixIssue runs one of Cinemask's fixes: "replace" or "subtitles".
func (c *Cinemask) FixIssue(ctx context.Context, id string, action string) (ok bool, msg string, after *Issue, err error) {
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Issue   *Issue `json:"issue"`
	}
	err = c.do(ctx, "POST", "/api/issues/"+id+"/fix", map[string]string{"action": action}, &out)
	return out.OK, out.Message, out.Issue, err
}

// SetIssue marks a Cinemask report fixed ("resolve", with a note) or open
// again ("reopen").
func (c *Cinemask) SetIssue(ctx context.Context, id, action, note string) error {
	var body any = map[string]string{}
	if action == "resolve" && note != "" {
		body = map[string]string{"note": note}
	}
	return c.do(ctx, "POST", "/api/issues/"+id+"/"+action, body, nil)
}

// IssuesLink is the page of Cinemask's reports.
func (c *Cinemask) IssuesLink() string { return c.base + "/issues" }

var cinemaskIssueTypes = map[string]string{"video": "video", "audio": "audio", "subtitles": "subtitles", "wrong": "wrong", "playback": "playback", "other": "other"}

// issueBody is what Cinemask is sent for a report: the movie, or the show
// with the season and (unless it's about the whole season) the episode,
// and a message saying who reported it in BingeBandit.
func issueBody(p problem) (map[string]any, error) {
	if p.TMDBID == 0 {
		return nil, errors.New("the title isn't matched to TMDB")
	}
	typ := cinemaskIssueTypes[p.Kind]
	if typ == "" {
		typ = "other"
	}
	b := map[string]any{"kind": "movie", "tmdbId": p.TMDBID, "type": typ}
	if p.ItemKind == "episode" {
		if p.Season == nil {
			return nil, errors.New("the episode has no season number")
		}
		b["kind"], b["season"] = "show", *p.Season
		if !p.WholeSeason && p.Episode != nil {
			b["episode"] = *p.Episode
		}
	}
	who := p.User
	if who == "" {
		who = "someone"
	}
	msg := "Reported in BingeBandit by " + who
	if p.Position > 0 {
		msg += " at " + clock(p.Position)
	}
	if note := strings.TrimSpace(p.Note); note != "" {
		msg += ": " + note
	}
	if r := []rune(msg); len(r) > 1000 {
		msg = string(r[:1000])
	}
	b["message"] = msg
	return b, nil
}

func clock(sec float64) string {
	s := int(sec)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func (a *app) reportsOn(ctx context.Context) bool { return a.conf(ctx)["reports"] != "false" }

func (a *app) problemsWith(ctx context.Context, status string) ([]problem, error) {
	var out []problem
	return out, a.p.Call(ctx, "GET", "/problems?status="+status, nil, &out)
}

func (a *app) updateProblem(ctx context.Context, id int64, body map[string]any) {
	if err := a.p.Call(ctx, "PUT", "/problems/"+strconv.FormatInt(id, 10), body, nil); err != nil {
		log.Printf("problem %d: telling BingeBandit: %v", id, err)
	}
}

// fileBackoff keeps a report Cinemask refused from being sent again every
// 2 minutes: it's tried again after an hour.
var (
	fileMu      sync.Mutex
	fileFailed  = map[int64]time.Time{}
	fileBackoff = time.Hour
)

// file sends a report to Cinemask and records it on BingeBandit's report.
// It returns Cinemask's report id.
func (a *app) file(ctx context.Context, cm *Cinemask, p problem) (string, error) {
	body, err := issueBody(p)
	if err != nil {
		return "", err
	}
	is, err := cm.FileIssue(ctx, body)
	if err != nil {
		return "", err
	}
	id := strconv.FormatInt(is.ID, 10)
	up := map[string]any{"remoteId": id, "remoteLink": cm.IssuesLink()}
	// Cinemask's automatic fixes may have started on it already.
	if st, reply := mirror(p, is); st != "" {
		up["status"], up["reply"] = st, reply
	}
	a.updateProblem(ctx, p.ID, up)
	return id, nil
}

// mirror says what BingeBandit's report should become to match Cinemask's
// (an empty status when it already does).
func mirror(p problem, is *Issue) (status, reply string) {
	if is.Status == p.Status || (is.Status != "open" && is.Status != "fixing" && is.Status != "resolved") {
		return "", ""
	}
	reply = is.resolution()
	if is.Status == "resolved" && reply == "" {
		reply = "Fixed in Cinemask."
	}
	return is.Status, reply
}

// fileNew sends a report made in BingeBandit on to Cinemask.
func (a *app) fileNew(ctx context.Context, data json.RawMessage) {
	var p problem
	if json.Unmarshal(data, &p) != nil || p.ID == 0 || p.RemoteID != "" || !a.reportsOn(ctx) {
		return
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		return
	}
	if _, err := a.file(ctx, cm, p); err != nil {
		log.Printf("problem %d: filing it in Cinemask: %v", p.ID, err)
		fileMu.Lock()
		fileFailed[p.ID] = time.Now()
		fileMu.Unlock()
	}
}

// syncReports files the open reports Cinemask hasn't got yet and brings
// Cinemask's state back to BingeBandit's reports.
func (a *app) syncReports(ctx context.Context) {
	if !a.reportsOn(ctx) {
		return
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		return
	}
	list, err := a.problemsWith(ctx, "open,fixing,resolved")
	if err != nil {
		log.Printf("problem reports: %v", err)
		return
	}
	var filed []problem
	for _, p := range list {
		if p.RemoteID != "" {
			filed = append(filed, p)
			continue
		}
		if p.Status == "resolved" {
			continue
		}
		fileMu.Lock()
		failed, ok := fileFailed[p.ID]
		fileMu.Unlock()
		if ok && time.Since(failed) < fileBackoff {
			continue
		}
		if _, err := a.file(ctx, cm, p); err != nil {
			log.Printf("problem %d: filing it in Cinemask: %v", p.ID, err)
			fileMu.Lock()
			fileFailed[p.ID] = time.Now()
			fileMu.Unlock()
		}
	}
	if len(filed) == 0 {
		return
	}
	issues, err := cm.Issues(ctx)
	if err != nil {
		log.Printf("problem reports: reading Cinemask's: %v", err)
		return
	}
	byID := map[string]*Issue{}
	for i := range issues {
		byID[strconv.FormatInt(issues[i].ID, 10)] = &issues[i]
	}
	for _, p := range filed {
		is := byID[p.RemoteID]
		if is == nil {
			continue // deleted in Cinemask, or too old to be listed
		}
		// Fixed in BingeBandit stays fixed: Cinemask missed being told
		// (the plugin was off), so it's told now.
		if p.Status == "resolved" {
			if is.Status != "resolved" {
				note := p.Reply
				if note == "" {
					note = "Marked fixed in BingeBandit."
				}
				if err := cm.SetIssue(ctx, p.RemoteID, "resolve", note); err != nil {
					log.Printf("problem %d: resolve in Cinemask: %v", p.ID, err)
				}
			}
			continue
		}
		if st, reply := mirror(p, is); st != "" {
			a.updateProblem(ctx, p.ID, map[string]any{"status": st, "reply": reply})
		}
	}
}

// onRefetch is an admin asking for a new copy (or subtitles) for a report.
func (a *app) onRefetch(ctx context.Context, data json.RawMessage) {
	var p problem
	if json.Unmarshal(data, &p) != nil || p.ID == 0 {
		return
	}
	status, reply := "fixing", ""
	cm, err := a.cinemask(ctx)
	if err == nil {
		status, reply, err = a.fixReport(ctx, cm, p)
	}
	if err != nil {
		status, reply = "open", "Cinemask couldn't fix it: "+err.Error()
	}
	a.updateProblem(ctx, p.ID, map[string]any{"status": status, "reply": reply})
}

// fixReport has Cinemask fix a report, filing it first when it hasn't been.
func (a *app) fixReport(ctx context.Context, cm *Cinemask, p problem) (status, reply string, err error) {
	// Cinemask has no fix for "something else": grab a copy it hasn't had.
	if p.Kind == "other" {
		if p.ItemKind == "episode" && p.WholeSeason {
			return "", "", errors.New("for a whole season, say what's wrong (picture, sound, subtitles…) so Cinemask knows how to fix it")
		}
		reply, err := refetch(ctx, cm, p)
		return "resolved", reply, err
	}
	id := p.RemoteID
	if id == "" {
		if id, err = a.file(ctx, cm, p); err != nil {
			return "", "", err
		}
	}
	action := "replace"
	if p.Kind == "subtitles" {
		action = "subtitles"
	}
	ok, msg, after, err := cm.FixIssue(ctx, id, action)
	if err != nil && strings.Contains(err.Error(), "already fixed") {
		// Fixed in Cinemask, but it's still wrong: open it again there.
		if err = cm.SetIssue(ctx, id, "reopen", ""); err == nil {
			ok, msg, after, err = cm.FixIssue(ctx, id, action)
		}
	}
	if err == nil && !ok && strings.Contains(msg, "isn't managed") {
		// Cinemask can only replace what it follows: start following it.
		kind := "movie"
		if p.ItemKind == "episode" {
			kind = "show"
		}
		if _, err = cm.Add(ctx, kind, p.TMDBID, 0, nil, false); err == nil {
			ok, msg, after, err = cm.FixIssue(ctx, id, action)
		}
	}
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", errors.New(msg)
	}
	status = "fixing"
	if after != nil && after.Status == "resolved" {
		status = "resolved"
	}
	return status, msg, nil
}

// onProblemAnswered keeps Cinemask's report in step when an admin marks
// one fixed or reopens it in BingeBandit.
func (a *app) onProblemAnswered(ctx context.Context, event string, data json.RawMessage) {
	var p problem
	if json.Unmarshal(data, &p) != nil || p.RemoteID == "" {
		return
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		return
	}
	action, note := "reopen", ""
	if event == "problem.resolved" {
		action, note = "resolve", p.Reply
		if note == "" {
			note = "Marked fixed in BingeBandit."
		}
	}
	if err := cm.SetIssue(ctx, p.RemoteID, action, note); err != nil && !strings.Contains(err.Error(), "already fixed") && !strings.Contains(err.Error(), "isn't fixed yet") {
		log.Printf("problem %d: %s in Cinemask: %v", p.ID, action, err)
	}
}
