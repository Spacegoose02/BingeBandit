package api

import (
	"net/http"
	"strings"
	"time"

	"bingebandit/internal/livetv"
	"bingebandit/internal/store"
)

type customReq struct {
	Number  float64        `json:"number"`
	Name    string         `json:"name"`
	Logo    string         `json:"logo"`
	Group   string         `json:"group"`
	Enabled bool           `json:"enabled"`
	Blocks  []*store.Block `json:"blocks"`
}

func (c *customReq) validate() string {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || c.Number <= 0 {
		return "a channel needs a name and a number above 0"
	}
	if len(c.Blocks) == 0 {
		return "add at least one block of content"
	}
	defaults := 0
	for _, b := range c.Blocks {
		if b.Mode != "shuffle" && b.Mode != "order" {
			return "each block plays in shuffle or order mode"
		}
		if len(b.Sources) == 0 {
			return "every block needs at least one source"
		}
		if b.StartMinute < 0 {
			defaults++
			b.StartMinute, b.EndMinute = -1, -1
		} else if b.StartMinute >= 1440 || b.EndMinute < 0 || b.EndMinute > 1440 || b.EndMinute == b.StartMinute {
			return "block times must be within the day and not empty"
		}
	}
	if defaults > 1 {
		return "only one all-day block is allowed"
	}
	return ""
}

func (s *Server) customChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	ch, err := s.Store.Channel(0, id)
	if err == nil && !ch.Custom {
		err = store.ErrNotFound
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	blocks, err := s.Store.CustomBlocks(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": ch, "blocks": blocks})
}

func (s *Server) saveCustomChannel(w http.ResponseWriter, r *http.Request) {
	var req customReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if msg := req.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	ch := &store.Channel{Number: req.Number, Name: req.Name, Logo: req.Logo, Group: req.Group, Enabled: req.Enabled}
	if id, ok := idParam(r, "id"); ok {
		existing, err := s.Store.Channel(0, id)
		if err == nil && !existing.Custom {
			err = store.ErrNotFound
		}
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		ch.ID = id
	}
	if err := s.Store.SaveCustomChannel(ch, req.Blocks); err != nil {
		writeStoreErr(w, err)
		return
	}
	// Whatever is airing finishes; everything after follows the new rules.
	now := time.Now()
	if err := s.Store.ClearPlayoutAfter(ch.ID, now.Unix()); err != nil {
		writeStoreErr(w, err)
		return
	}
	n, err := livetv.BuildPlayout(s.Store, ch.ID, now)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "saved, but the schedule is empty: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": ch, "scheduled": n})
}

func (s *Server) customSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	now := time.Now().Unix()
	progs, err := s.Store.PlayoutPrograms(id, now, now+24*3600)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, progs)
}

type sourceOption struct {
	Type   string `json:"type"`
	ID     int64  `json:"id,omitempty"`
	Label  string `json:"label,omitempty"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// contentSources lists what a custom channel block can draw from. Shows come
// from a search (?q=) since libraries can hold thousands.
func (s *Server) contentSources(w http.ResponseWriter, r *http.Request) {
	out := []sourceOption{}
	libs, err := s.Store.Libraries()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	for _, l := range libs {
		out = append(out, sourceOption{Type: "library", ID: l.ID, Name: l.Name, Detail: "whole library"})
		if cols, err := s.Store.LibraryCollections(l.ID); err == nil {
			for _, c := range cols {
				out = append(out, sourceOption{Type: "collection", ID: c.ID, Name: c.Name, Detail: "collection · " + l.Name})
			}
		}
		if labels, err := s.Store.LibraryLabels(l.ID); err == nil {
			for _, lb := range labels {
				out = append(out, sourceOption{Type: "label", Label: lb, Name: lb, Detail: "label · " + l.Name})
			}
		}
	}
	if lists, err := s.Store.Playlists(currentUser(r).ID); err == nil {
		for _, p := range lists {
			out = append(out, sourceOption{Type: "playlist", ID: p.ID, Name: p.Name, Detail: "your playlist"})
		}
	}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		if items, err := s.Store.Search(0, q, 30); err == nil {
			for _, it := range items {
				if it.Kind == "show" {
					out = append(out, sourceOption{Type: "show", ID: it.ID, Name: it.Title, Detail: "show"})
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}
