package main

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"
)

// Title requests: when an admin approves a movie or show someone asked for
// in BingeBandit, Cinemask monitors it and searches for it at once, and
// BingeBandit is told it was sent (or why it couldn't be). Approved
// requests are picked up on the request.approved event and every 15
// minutes, so ones approved while the plugin was off or Cinemask was down
// go through later.

// request is an approved request, as the plugin API lists it.
type request struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	TMDBID int64  `json:"tmdbId"`
	Title  string `json:"title"`
}

var requestInterval = 15 * time.Minute

func (a *app) requestsOn(ctx context.Context) bool { return a.conf(ctx)["requests"] != "false" }

func (a *app) approvedRequests(ctx context.Context) ([]request, error) {
	var out []request
	return out, a.p.Call(ctx, "GET", "/requests?status=approved", nil, &out)
}

func (a *app) reportRequest(ctx context.Context, id int64, status, reason, link string) error {
	return a.p.Call(ctx, "PUT", "/requests/"+strconv.FormatInt(id, 10),
		map[string]string{"status": status, "reason": reason, "link": link}, nil)
}

func (a *app) watchRequests(ctx context.Context) {
	t := time.NewTicker(requestInterval)
	defer t.Stop()
	for {
		a.sendRequests(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// requestsMu keeps two runs (an event and the ticker) from sending the
// same request twice.
var requestsMu sync.Mutex

// sendRequests hands every approved request to Cinemask.
func (a *app) sendRequests(ctx context.Context) {
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if a.listRequests == nil || !a.requestsOn(ctx) {
		return
	}
	list, err := a.listRequests(ctx)
	if err != nil {
		log.Printf("requests: %v", err)
		return
	}
	if len(list) == 0 {
		return
	}
	cm, err := a.cinemask(ctx)
	if err != nil {
		// Not set up yet: they stay approved until it is.
		log.Printf("requests: %d waiting: %v", len(list), err)
		return
	}
	for _, r := range list {
		if ctx.Err() != nil {
			return
		}
		status, reason := "sent", ""
		if err := sendRequest(ctx, cm, r); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			status, reason = "failed", err.Error()
			log.Printf("request %q: %v", r.Title, err)
		}
		if err := a.setRequest(ctx, r.ID, status, reason, cm.Link(r.Kind, r.TMDBID)); err != nil {
			log.Printf("request %q: telling BingeBandit: %v", r.Title, err)
		}
	}
}

// sendRequest has Cinemask monitor the title and search for it, or just
// search when it already monitors it.
func sendRequest(ctx context.Context, cm *Cinemask, r request) error {
	if r.Kind != "movie" && r.Kind != "show" {
		return errors.New("only movies and shows can be fetched")
	}
	have, err := cm.Lookup(ctx, r.Kind, r.TMDBID)
	if err != nil {
		return err
	}
	if have == nil {
		_, err = cm.Monitor(ctx, r.Kind, r.TMDBID, 0, "", true)
		return err
	}
	res, err := cm.Search(ctx, have.ID, Scope{})
	if err == nil && len(res.Errors) > 0 {
		err = errors.New(res.Errors[0])
	}
	return err
}
