package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"bingebandit/internal/importer"
	"bingebandit/internal/store"
)

// importReq says where to import from. Credentials are used for this import
// only and never stored.
type importReq struct {
	Source string `json:"source"` // plex | tautulli | tautulli-db | cinemask
	URL    string `json:"url"`
	Token  string `json:"token"` // Plex token or Tautulli API key
	Path   string `json:"path"`  // tautulli.db on this server
}

func (q importReq) build() (importer.Source, error) {
	needURL := func() (string, error) {
		u, err := url.Parse(strings.TrimSpace(q.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", errors.New("enter the server address, like http://192.168.1.10:32400")
		}
		if strings.TrimSpace(q.Token) == "" {
			return "", errors.New("enter the token or API key")
		}
		return strings.TrimRight(u.String(), "/"), nil
	}
	switch q.Source {
	case "plex":
		u, err := needURL()
		if err != nil {
			return nil, err
		}
		return importer.NewPlex(u, strings.TrimSpace(q.Token)), nil
	case "tautulli":
		u, err := needURL()
		if err != nil {
			return nil, err
		}
		return importer.NewTautulliAPI(u, strings.TrimSpace(q.Token)), nil
	case "cinemask":
		u, err := needURL()
		if err != nil {
			return nil, err
		}
		return importer.NewCinemask(u, strings.TrimSpace(q.Token)), nil
	case "tautulli-db":
		if strings.TrimSpace(q.Path) == "" {
			return nil, errors.New("enter the path to tautulli.db on this server")
		}
		return &importer.TautulliDB{Path: strings.TrimSpace(q.Path)}, nil
	}
	return nil, errors.New("unknown source")
}

// importConnect checks the source and lists its users, each with a suggested
// BingeBandit user (same name, ignoring case).
func (s *Server) importConnect(w http.ResponseWriter, r *http.Request) {
	var req importReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	src, err := req.build()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	users, err := src.Users(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	bb, err := s.Store.ListUsers()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	type row struct {
		importer.SourceUser
		Suggest int64 `json:"suggest"`
	}
	out := []row{}
	for _, u := range users {
		var sug int64
		for _, b := range bb {
			if strings.EqualFold(b.Username, u.Name) {
				sug = b.ID
			}
		}
		out = append(out, row{u, sug})
	}
	// Cinemask's artwork is its collection posters.
	writeJSON(w, http.StatusOK, map[string]any{"users": out, "collections": req.Source == "plex" || req.Source == "cinemask", "labels": req.Source == "plex",
		"watchedState": req.Source == "plex", "artwork": req.Source == "plex" || req.Source == "cinemask"})
}

func (s *Server) importStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		importReq
		importer.Options
		// Users maps source user → BingeBandit user id; -1 creates a new user.
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	src, err := req.build()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !req.History && !req.Watched && !req.Collections && !req.Artwork && !req.Labels {
		writeErr(w, http.StatusBadRequest, "choose something to import")
		return
	}
	if s.Importer.Status().Running {
		writeErr(w, http.StatusConflict, "an import is already running")
		return
	}
	users := map[string]int64{}
	for name, id := range req.Users {
		switch {
		case id == -1 && !req.DryRun:
			u, err := s.Store.CreateUser(s.uniqueUsername(name), "", false)
			if err != nil {
				writeStoreErr(w, err)
				return
			}
			log.Printf("import: created user %q for %s user %q", u.Username, req.Source, name)
			users[name] = u.ID
		case id == -1:
			users[name] = -1 // dry run: count as mapped without creating anyone
		case id > 0:
			if _, err := s.Store.UserByID(id); err != nil {
				writeErr(w, http.StatusBadRequest, fmt.Sprintf("user %d doesn't exist", id))
				return
			}
			users[name] = id
		}
	}
	req.Options.Users = users
	if err := s.Importer.Start(s.ctx, src, req.Options); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.Importer.Status())
}

func (s *Server) importStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Importer.Status())
}

func (s *Server) importCancel(w http.ResponseWriter, r *http.Request) {
	s.Importer.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

// uniqueUsername cleans a name from another system into a free username.
func (s *Server) uniqueUsername(want string) string {
	base := strings.Trim(reUsernameJunk.ReplaceAllString(want, "."), ".")
	if len(base) < 2 {
		base = "user"
	}
	if len(base) > 48 {
		base = base[:48]
	}
	name := base
	for i := 2; ; i++ {
		if _, err := s.Store.UserByUsername(name); errors.Is(err, store.ErrNotFound) {
			return name
		}
		name = fmt.Sprintf("%s%d", base, i)
	}
}
