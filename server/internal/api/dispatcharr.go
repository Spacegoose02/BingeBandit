package api

import (
	"net/http"
	"net/url"
	"strings"

	"bingebandit/internal/livetv"
)

// dispatcharrReq says which Dispatcharr to import from. The login is used
// for this import only and never stored.
type dispatcharrReq struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
	APIKey   string `json:"apiKey"`
}

func (q dispatcharrReq) client() (*livetv.Dispatcharr, string) {
	u, err := url.Parse(strings.TrimSpace(q.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "enter Dispatcharr's address, like http://192.168.1.10:9191"
	}
	if strings.TrimSpace(q.APIKey) == "" && (strings.TrimSpace(q.Username) == "" || q.Password == "") {
		return nil, "enter an admin username and password, or an API key"
	}
	return livetv.NewDispatcharr(u.String(), strings.TrimSpace(q.Username), q.Password, strings.TrimSpace(q.APIKey)), ""
}

func (s *Server) dispatcharrSetup(w http.ResponseWriter, r *http.Request) *livetv.DispatcharrSetup {
	var req dispatcharrReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return nil
	}
	d, msg := req.client()
	if msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return nil
	}
	setup, err := d.Fetch(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return nil
	}
	return setup
}

// dispatcharrPreview signs in to Dispatcharr and reports what would be imported.
func (s *Server) dispatcharrPreview(w http.ResponseWriter, r *http.Request) {
	if setup := s.dispatcharrSetup(w, r); setup != nil {
		writeJSON(w, http.StatusOK, setup.Plan())
	}
}

// dispatcharrImport reads Dispatcharr again and imports in the background.
func (s *Server) dispatcharrImport(w http.ResponseWriter, r *http.Request) {
	if s.dispatcharr.Status().Running {
		writeErr(w, http.StatusConflict, "an import is already running")
		return
	}
	setup := s.dispatcharrSetup(w, r)
	if setup == nil {
		return
	}
	if err := s.dispatcharr.Start(s.ctx, s.Store, setup); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.dispatcharr.Status())
}

func (s *Server) dispatcharrStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.dispatcharr.Status())
}
