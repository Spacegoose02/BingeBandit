package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/plugins"
	"bingebandit/internal/store"
)

// pluginCatalogURLs are the official catalogue and the admin's own, in that order.
func (s *Server) pluginCatalogURLs() []string {
	urls := []string{s.Plugins.Official}
	for _, line := range strings.Split(s.Store.Setting(store.SettingPluginCatalogs), "\n") {
		line = strings.TrimSpace(line)
		if (strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://")) && !slices.Contains(urls, line) {
			urls = append(urls, line)
		}
	}
	return urls
}

func (s *Server) pluginCatalog(w http.ResponseWriter, r *http.Request) {
	urls := s.pluginCatalogURLs()
	list, errs := s.Plugins.Catalogs(urls, r.URL.Query().Get("refresh") != "")
	for i, e := range errs {
		if e.URL == s.Plugins.Official && strings.HasPrefix(e.Error, "404") {
			errs[i].Error = "the latest BingeBandit release has no plugin catalogue yet; it comes with the next release"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plugins": list, "errors": errs, "catalogs": urls, "official": s.Plugins.Official,
		"platform": plugins.Platform(), "installDir": s.Plugins.InstallDir,
	})
}

func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Catalog string `json:"catalog"`
		ID      string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil || req.ID == "" {
		writeErr(w, http.StatusBadRequest, "send {catalog, id}")
		return
	}
	// Only from catalogues the admin has listed.
	if !slices.Contains(s.pluginCatalogURLs(), req.Catalog) {
		writeErr(w, http.StatusBadRequest, "that catalogue isn't in the list")
		return
	}
	if err := s.Plugins.Install(req.Catalog, req.ID); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.Plugins.Settle(req.ID, 5*time.Second)
	st, err := s.Plugins.Get(req.ID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) uninstallPlugin(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.Store.Plugin(id); err != nil {
		writeStoreErr(w, err)
		return
	}
	if err := s.Plugins.Uninstall(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeStoreErr(w, err)
		} else {
			writeErr(w, http.StatusConflict, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
