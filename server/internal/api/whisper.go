package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"bingebandit/internal/store"
	"bingebandit/internal/whisper"
)

// Subtitles made from a file's sound with whisper.cpp (an admin's button in
// the player's Find subtitles). It takes a while (minutes for a film on a
// fast CPU), so it runs in the background and the player asks how it's
// going. The result is saved beside the video as Name.<lang>.whisper.srt,
// like a downloaded subtitle, so every app sees it.

type whisperJob struct {
	Status   string         `json:"status"` // running, done, failed
	Step     string         `json:"step,omitempty"`
	Progress float64        `json:"progress"`
	Error    string         `json:"error,omitempty"`
	Index    int            `json:"index,omitempty"`   // the new subtitle stream, once done
	Streams  []store.Stream `json:"streams,omitempty"` // the file's streams with it
}

var (
	whisperMu   sync.Mutex
	whisperJobs = map[int64]*whisperJob{}
)

func (s *Server) whisperRunner() *whisper.Runner {
	s.whisperOnce.Do(func() {
		s.whisper = &whisper.Runner{FFmpeg: s.FFmpeg, DataDir: s.DataDir, Model: func() string { return s.Store.Setting(store.SettingWhisperModel) }}
	})
	return s.whisper
}

func whisperStatus(id int64) whisperJob {
	whisperMu.Lock()
	defer whisperMu.Unlock()
	if j := whisperJobs[id]; j != nil {
		return *j
	}
	return whisperJob{Status: "idle"}
}

// whisperState answers GET /admin/files/{id}/whisper: whether whisper.cpp
// is installed, and the file's job.
func (s *Server) whisperState(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": whisper.Available(), "job": whisperStatus(id)})
}

// startWhisper answers POST /admin/files/{id}/whisper.
func (s *Server) startWhisper(w http.ResponseWriter, r *http.Request) {
	mf := s.playableFile(w, r)
	if mf == nil {
		return
	}
	var req struct {
		Language    string `json:"language"`    // two letters, or "" to detect it
		Translate   bool   `json:"translate"`   // English subtitles whatever is spoken
		AudioStream int    `json:"audioStream"` // the file's stream index; -1 or 0: the first audio stream
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Language = strings.ToLower(strings.TrimSpace(req.Language))
	if req.Language != "" && (len(req.Language) != 2 || strings.ContainsAny(req.Language, `./\ `)) {
		writeErr(w, http.StatusBadRequest, "use a two-letter language code, or none to detect it")
		return
	}
	if !whisper.Available() {
		writeErr(w, http.StatusNotImplemented, whisper.ErrNoWhisper.Error())
		return
	}
	// The stream index among the file's audio streams, which is how ffmpeg counts them.
	nth := 0
	for _, st := range mf.Streams {
		if st.Type != "audio" {
			continue
		}
		if st.Index == req.AudioStream {
			break
		}
		nth++
	}
	if nth >= countAudio(mf.Streams) {
		nth = 0
	}
	whisperMu.Lock()
	if j := whisperJobs[mf.ID]; j != nil && j.Status == "running" {
		whisperMu.Unlock()
		writeJSON(w, http.StatusAccepted, whisperStatus(mf.ID))
		return
	}
	job := &whisperJob{Status: "running", Step: "Waiting for another file to finish"}
	whisperJobs[mf.ID] = job
	whisperMu.Unlock()

	go func() {
		set := func(f func(j *whisperJob)) {
			whisperMu.Lock()
			f(job)
			whisperMu.Unlock()
		}
		res, err := s.whisperRunner().Run(context.Background(), whisper.Request{
			Path: mf.Path, Stream: nth, Language: req.Language, Translate: req.Translate, Duration: mf.Duration,
		}, func(step string, done float64) {
			set(func(j *whisperJob) { j.Step, j.Progress = step, done })
		})
		if err == nil {
			tags := "whisper"
			if res.Language != "und" {
				tags = res.Language + ".whisper"
			}
			var streams []store.Stream
			var index int
			streams, index, err = s.Scanner.SaveSubtitle(mf, tags, false, res.SRT)
			if err == nil {
				set(func(j *whisperJob) { j.Status, j.Step, j.Progress, j.Index, j.Streams = "done", "", 1, index, streams })
				return
			}
		}
		log.Printf("whisper: %s: %v", mf.Path, err)
		msg := err.Error()
		if errors.Is(err, context.Canceled) {
			msg = "stopped"
		}
		set(func(j *whisperJob) { j.Status, j.Error = "failed", msg })
	}()
	writeJSON(w, http.StatusAccepted, whisperStatus(mf.ID))
}

func countAudio(streams []store.Stream) int {
	n := 0
	for _, st := range streams {
		if st.Type == "audio" {
			n++
		}
	}
	return n
}
