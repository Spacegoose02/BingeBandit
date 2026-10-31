package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"bingebandit/internal/library"
	"bingebandit/internal/opensubs"
)

// subtitleSearchAllowed turns away kid profiles: downloads write files into
// the library and use the server's daily OpenSubtitles allowance.
func (s *Server) subtitleSearchAllowed(w http.ResponseWriter, r *http.Request) bool {
	if s.OpenSubs == nil {
		writeErr(w, http.StatusNotFound, "subtitle search is not available")
		return false
	}
	if currentUser(r).Kid {
		writeErr(w, http.StatusForbidden, "kid profiles can't download subtitles")
		return false
	}
	return true
}

func writeOpenSubsErr(w http.ResponseWriter, err error) {
	var e *opensubs.Error
	switch {
	case errors.Is(err, opensubs.ErrNoKey):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.As(err, &e) && e.Status == http.StatusNotAcceptable:
		writeErr(w, http.StatusTooManyRequests, "Today's OpenSubtitles downloads are used up: "+e.Message)
	case errors.As(err, &e):
		writeErr(w, http.StatusBadGateway, e.Message)
	default:
		writeErr(w, http.StatusBadGateway, err.Error())
	}
}

// searchSubtitles looks the file up on OpenSubtitles. ?lang= is a language
// name or code (default English).
func (s *Server) searchSubtitles(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleSearchAllowed(w, r) {
		return
	}
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	uid := currentUser(r).ID
	it, err := s.Store.Item(uid, mf.ItemID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	lang := library.LanguageCode(r.URL.Query().Get("lang"))
	if lang == "" {
		lang = "eng"
	}
	code := opensubs.Code(lang)
	if code == "" {
		writeErr(w, http.StatusBadRequest, "OpenSubtitles doesn't know that language")
		return
	}
	q := opensubs.Query{Languages: strings.Split(code, ","), Title: it.Title, Year: it.Year, Hash: opensubs.Hash(mf.Path)}
	if it.Kind == "episode" && it.SeasonNumber != nil && it.EpisodeNumber != nil {
		q.Episode, q.Season, q.Number = true, *it.SeasonNumber, *it.EpisodeNumber
		q.Title = it.ShowTitle
		if show, err := s.Store.Item(uid, it.ShowID); err == nil {
			q.TMDBID, q.IMDbID, q.Title = show.TMDBID, show.IMDbID, show.Title
		}
	} else {
		q.TMDBID, q.IMDbID = it.TMDBID, it.IMDbID
	}
	results, err := s.OpenSubs.Search(r.Context(), q)
	if err != nil {
		writeOpenSubsErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"language": lang, "results": results})
}

// downloadSubtitle saves an OpenSubtitles result beside the video and adds it
// to the file's subtitles, answering with the new stream list and its index.
func (s *Server) downloadSubtitle(w http.ResponseWriter, r *http.Request) {
	if !s.subtitleSearchAllowed(w, r) {
		return
	}
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	var in struct {
		FileID          int64  `json:"fileId"`
		Language        string `json:"language"`
		HearingImpaired bool   `json:"hearingImpaired"`
	}
	if err := readJSON(r, &in); err != nil || in.FileID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	// "pt-BR" → "pt"; the library reads two-letter codes in file names.
	lang, _, _ := strings.Cut(strings.ToLower(in.Language), "-")
	if lang == "" || strings.ContainsAny(lang, `./\ `) {
		lang = "sub"
	}
	data, err := s.OpenSubs.Download(r.Context(), in.FileID)
	if err != nil {
		writeOpenSubsErr(w, err)
		return
	}
	streams, index, err := s.Scanner.SaveSubtitle(mf, lang, in.HearingImpaired, data)
	if err != nil {
		log.Printf("subtitle download for file %d: %v", mf.ID, err)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": streams, "index": index})
}
