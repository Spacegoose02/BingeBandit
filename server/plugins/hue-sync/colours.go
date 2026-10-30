package main

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"time"

	"bingebandit/pkg/bbplugin"
)

// readers caps how many ffmpegs the TVs that drive their own hub can run at once.
var readers = make(chan struct{}, 4)

// colours reads a stretch of a file's picture for a TV that drives its own
// Hue hub (one in another home the server can't reach): GET
// /me/colours?fileId=&from=&seconds= gives the colour of each zone at
// sampleFPS, and the TV sends them to its lights as the video plays.
func (a *app) colours(w http.ResponseWriter, r *http.Request) {
	if _, ok := bbplugin.UserOf(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in first"})
		return
	}
	q := r.URL.Query()
	fileID, err := strconv.ParseInt(q.Get("fileId"), 10, 64)
	from, err2 := strconv.ParseFloat(q.Get("from"), 64)
	seconds, err3 := strconv.ParseFloat(q.Get("seconds"), 64)
	if err != nil || err2 != nil || err3 != nil || fileID <= 0 || from < 0 || seconds <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "send fileId, from and seconds"})
		return
	}
	seconds = min(seconds, 60)
	select {
	case readers <- struct{}{}:
		defer func() { <-readers }()
	case <-r.Context().Done():
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	url, key := a.fileURL(fileID)
	args := ffmpegArgs(url, key, from, sampleFPS)
	// Stop after the stretch asked for: -t goes before the output.
	args = append(args[:len(args)-1], "-t", strconv.FormatFloat(seconds, 'f', 2, 64), "pipe:1")
	cmd := exec.CommandContext(ctx, a.ffmpeg, args...)
	stderr := &tail{}
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	frames := []map[string][3]float64{}
	buf := make([]byte, frameW*frameH*3)
	for {
		if _, err := io.ReadFull(out, buf); err != nil {
			break
		}
		z := map[string][3]float64{}
		for name, c := range zoneColours(buf) {
			z[name] = [3]float64{round4(c.X), round4(c.Y), round4(c.Bri)}
		}
		frames = append(frames, z)
	}
	if err := cmd.Wait(); err != nil && len(frames) == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "can't read the video: " + stderr.String()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "fps": sampleFPS, "frames": frames})
}
