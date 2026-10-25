package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"
)

// bulkState is the "Search all" job: one title at a time, so Cinemask and
// its indexers aren't flooded.
type bulkState struct {
	Running bool      `json:"running"`
	Total   int       `json:"total"`
	Done    int       `json:"done"`
	Current string    `json:"current,omitempty"`
	Grabbed int       `json:"grabbed"`
	Skipped int       `json:"skipped"` // not in Cinemask, or no TMDB match
	Errors  []string  `json:"errors"`
	Started time.Time `json:"started"`
	cancel  context.CancelFunc
}

// pause between titles in a bulk search.
var bulkPause = 5 * time.Second

type bulkTitle struct {
	kind   string
	tmdbID int64
	title  string
}

func (a *app) startBulk(rep *report, c *Cinemask) error {
	var titles []bulkTitle
	for _, m := range rep.Movies {
		titles = append(titles, bulkTitle{"movie", m.TMDBID, m.Title})
	}
	for _, s := range rep.Shows {
		titles = append(titles, bulkTitle{"show", s.TMDBID, s.Title})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bulk.Running {
		return fmt.Errorf("already searching (%d of %d done)", a.bulk.Done, a.bulk.Total)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.bulk = bulkState{Running: true, Total: len(titles), Errors: []string{}, Started: time.Now(), cancel: cancel}
	go a.runBulk(ctx, c, titles)
	return nil
}

func (a *app) runBulk(ctx context.Context, c *Cinemask, titles []bulkTitle) {
	defer func() {
		a.mu.Lock()
		a.bulk.Running, a.bulk.Current, a.bulk.cancel = false, "", nil
		b := a.bulk
		a.mu.Unlock()
		log.Printf("search all: %d titles, %d grabbed, %d skipped, %d errors", b.Done, b.Grabbed, b.Skipped, len(b.Errors))
	}()
	for i, t := range titles {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(bulkPause):
			}
		}
		a.mu.Lock()
		a.bulk.Current = t.title
		a.mu.Unlock()
		grabbed, skipped, err := a.searchOne(ctx, c, t)
		a.mu.Lock()
		a.bulk.Done++
		a.bulk.Grabbed += grabbed
		if skipped {
			a.bulk.Skipped++
		}
		if err != nil && len(a.bulk.Errors) < 50 {
			a.bulk.Errors = append(a.bulk.Errors, t.title+": "+err.Error())
		}
		a.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
	}
}

func (a *app) searchOne(ctx context.Context, c *Cinemask, t bulkTitle) (grabbed int, skipped bool, err error) {
	if t.tmdbID == 0 {
		return 0, true, nil
	}
	m, err := c.Lookup(ctx, t.kind, t.tmdbID)
	if err != nil || m == nil {
		return 0, true, err
	}
	res, err := c.Search(ctx, m.ID, Scope{})
	if err == nil && len(res.Errors) > 0 {
		err = fmt.Errorf("%s", res.Errors[0])
	}
	return res.Grabbed, false, err
}

func (a *app) stopBulk() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bulk.cancel != nil {
		a.bulk.cancel()
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
