package api

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"bingebandit/internal/store"
	"bingebandit/internal/stream"
)

// Offline downloads for the phone app: the app asks for a copy of a file
// (the original, or a one-off conversion at a chosen height), saves it, and
// plays it with no connection. Downloads follow the same rules as "Download
// original file": admins always, everyone else when the server allows
// downloads, and only titles the profile can see (library access and kid
// approvals, through store.visible).

type offlineCopies struct {
	once sync.Once
	d    *stream.Downloads
}

func (s *Server) downloads() *stream.Downloads {
	s.offline.once.Do(func() { s.offline.d = stream.NewDownloads(s.Store, s.FFmpeg, s.DataDir) })
	return s.offline.d
}

// offlineReq is POST /api/files/{id}/offline: height 0 is the original file.
type offlineReq struct {
	Height       int  `json:"height"`
	AudioStream  *int `json:"audioStream"`
	BurnSubtitle *int `json:"burnSubtitle"`
}

// offlineResp says whether the copy can be downloaded yet: status is ready
// (with url and size), queued or preparing (with progress 0..1), or failed.
type offlineResp struct {
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	URL      string  `json:"url,omitempty"`
	Size     int64   `json:"size,omitempty"`
	Height   int     `json:"height"`
	Error    string  `json:"error,omitempty"`
}

func (s *Server) prepareOffline(w http.ResponseWriter, r *http.Request) {
	if !s.canDownload(currentUser(r)) {
		writeErr(w, http.StatusForbidden, "downloads are turned off on this server")
		return
	}
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	var req offlineReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Height <= 0 {
		writeJSON(w, http.StatusOK, offlineResp{Status: "ready", Progress: 1, URL: fmt.Sprintf("/api/files/%d/direct?download=1", mf.ID), Size: mf.Size, Height: mf.Height})
		return
	}
	if _, err := os.Stat(mf.Path); err != nil {
		writeErr(w, http.StatusNotFound, "file is not available")
		return
	}
	o := stream.DownloadOptions{Height: req.Height, AudioStream: -1, BurnSubtitle: -1}
	if req.AudioStream != nil {
		o.AudioStream = *req.AudioStream
	}
	if req.BurnSubtitle != nil {
		o.BurnSubtitle = *req.BurnSubtitle
	}
	j := s.downloads().Prepare(mf, o)
	resp := offlineResp{Status: j.Status, Progress: j.Progress, Size: j.Size, Error: j.Error, Height: min(req.Height, mf.Height)}
	if mf.Height == 0 {
		resp.Height = req.Height
	}
	if j.Status == "ready" {
		resp.URL = "/api/offline/" + j.Key
	}
	writeJSON(w, http.StatusOK, resp)
}

// offlineFile sends a finished copy (with ranges, so the app can resume).
func (s *Server) offlineFile(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	fileID, ok := stream.KeyFileID(key)
	if !ok {
		writeErr(w, http.StatusNotFound, "no such download")
		return
	}
	if !s.canDownload(currentUser(r)) {
		writeErr(w, http.StatusForbidden, "downloads are turned off on this server")
		return
	}
	mf, err := s.Store.File(fileID)
	if err == nil {
		_, err = s.Store.Item(currentUser(r).ID, mf.ItemID)
	}
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	path := s.downloads().File(key)
	if path == "" {
		writeErr(w, http.StatusNotFound, "this download isn't ready; ask for it again")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "this download isn't ready; ask for it again")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cannot read file")
		return
	}
	name := strings.TrimSuffix(st.Name(), ".mp4") + ".mp4"
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// checkOffline tells the app which of its downloaded titles this profile
// may still keep: ones it can still see, while downloads are allowed. The
// app deletes the rest. It also says which of them the profile has
// watched (anywhere), for the app's automatic downloads of next episodes.
func (s *Server) checkOffline(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []int64 `json:"items"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u := currentUser(r)
	allowed, watched := []int64{}, []int64{}
	can := s.canDownload(u)
	if can {
		for _, id := range req.Items {
			it, err := s.Store.Item(u.ID, id)
			if err == nil {
				allowed = append(allowed, id)
				if it.Watch.Watched {
					watched = append(watched, id)
				}
			} else if !errors.Is(err, store.ErrNotFound) {
				writeStoreErr(w, err) // never tell the app to delete over a database hiccup
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"canDownload": can, "allowed": allowed, "watched": watched})
}
