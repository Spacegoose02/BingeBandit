package stream

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/media"
	"bingebandit/internal/store"
)

// Download copies are one-off conversions for offline viewing on phones and
// tablets: a single MP4 (H.264 + stereo AAC, fast start) at a chosen height,
// made in the background and kept in the cache until nobody has asked for
// it for a while. The original file needs no copy; the app saves it as is.

// downloadKeep is how long a finished copy stays after it was last asked for.
const downloadKeep = 7 * 24 * time.Hour

// downloadKbps is the video bitrate of a copy at each height: lower than
// streaming, since it has to fit on a phone.
func downloadKbps(h int) int {
	switch {
	case h >= 1080:
		return 5000
	case h >= 720:
		return 2500
	default:
		return 1000
	}
}

// DownloadOptions describe a copy: its height (no taller than the file),
// the audio stream (-1: the file's default) and an image subtitle stream to
// burn in (-1: none). Text subtitles are saved by the app separately.
type DownloadOptions struct {
	Height       int `json:"height"`
	AudioStream  int `json:"audioStream"`
	BurnSubtitle int `json:"burnSubtitle"`
}

// DownloadJob is a copy being made or ready.
type DownloadJob struct {
	Key      string  `json:"key"`
	FileID   int64   `json:"fileId"`
	Status   string  `json:"status"` // queued, preparing, ready, failed
	Progress float64 `json:"progress"`
	Size     int64   `json:"size,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// Downloads makes download copies, one at a time.
type Downloads struct {
	st     *store.Store
	ffmpeg string
	dir    string

	mu    sync.Mutex
	jobs  map[string]*DownloadJob
	queue chan downloadTask
	used  map[string]time.Time
}

type downloadTask struct {
	job  *DownloadJob
	file store.MediaFile
	opts DownloadOptions
}

// NewDownloads starts the worker; copies go in dataDir/cache/downloads.
func NewDownloads(st *store.Store, ffmpeg, dataDir string) *Downloads {
	d := &Downloads{
		st:     st,
		ffmpeg: ffmpeg,
		dir:    filepath.Join(dataDir, "cache", "downloads"),
		jobs:   map[string]*DownloadJob{},
		queue:  make(chan downloadTask, 256),
		used:   map[string]time.Time{},
	}
	os.MkdirAll(d.dir, 0o755)
	// Half-made copies from before a restart are useless.
	if parts, _ := filepath.Glob(filepath.Join(d.dir, "*.part.mp4")); len(parts) > 0 {
		for _, p := range parts {
			os.Remove(p)
		}
	}
	go d.worker()
	go d.janitor()
	return d
}

// DownloadKey names the copy of f with o. The file's size is in it, so a
// replaced file gets a new copy.
func DownloadKey(f *store.MediaFile, o DownloadOptions) string {
	return fmt.Sprintf("%d-%d-%d-%d-%d", f.ID, f.Size, o.Height, o.AudioStream, o.BurnSubtitle)
}

// KeyFileID is the media file a key belongs to.
func KeyFileID(key string) (int64, bool) {
	id, _, ok := strings.Cut(key, "-")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	return n, err == nil && n > 0
}

func (d *Downloads) path(key string) string { return filepath.Join(d.dir, key+".mp4") }

// Prepare returns the copy's state, queueing it when it isn't made yet. A
// failed copy is tried again.
func (d *Downloads) Prepare(f *store.MediaFile, o DownloadOptions) DownloadJob {
	if f.Height > 0 && (o.Height <= 0 || o.Height > f.Height) {
		o.Height = f.Height
	}
	key := DownloadKey(f, o)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.used[key] = time.Now()
	if j, ok := d.jobs[key]; ok && j.Status != "failed" {
		return *j
	}
	if st, err := os.Stat(d.path(key)); err == nil {
		j := &DownloadJob{Key: key, FileID: f.ID, Status: "ready", Progress: 1, Size: st.Size()}
		d.jobs[key] = j
		return *j
	}
	j := &DownloadJob{Key: key, FileID: f.ID, Status: "queued"}
	select {
	case d.queue <- downloadTask{job: j, file: *f, opts: o}:
	default:
		j.Status, j.Error = "failed", "too many downloads are being prepared; try again later"
	}
	d.jobs[key] = j
	return *j
}

// File is the path of a ready copy, or "" when there isn't one.
func (d *Downloads) File(key string) string {
	if strings.ContainsAny(key, `/\.`) {
		return ""
	}
	p := d.path(key)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	d.mu.Lock()
	d.used[key] = time.Now()
	d.mu.Unlock()
	return p
}

func (d *Downloads) set(j *DownloadJob, fn func(*DownloadJob)) {
	d.mu.Lock()
	fn(j)
	d.mu.Unlock()
}

func (d *Downloads) worker() {
	for t := range d.queue {
		d.set(t.job, func(j *DownloadJob) { j.Status = "preparing" })
		size, err := d.make(t)
		d.set(t.job, func(j *DownloadJob) {
			if err != nil {
				j.Status, j.Error = "failed", err.Error()
				return
			}
			j.Status, j.Progress, j.Size = "ready", 1, size
		})
	}
}

// make runs ffmpeg for one copy, with the admin's hardware encoder, and
// again in software if that fails.
func (d *Downloads) make(t downloadTask) (int64, error) {
	out := d.path(t.job.Key)
	part := filepath.Join(d.dir, t.job.Key+".part.mp4")
	encoder := d.st.Setting(store.SettingHWEncoder)
	device := d.st.Setting(store.SettingHWDevice)
	tonemap := d.st.Setting(store.SettingTonemap) == "true" && t.file.HDR
	algo := tonemapAlgorithm(d.st.Setting(store.SettingTonemapAlgorithm))
	var err error
	for _, enc := range []string{encoder, "libx264"} {
		args := DownloadArgs(&t.file, t.opts, enc, device, tonemap, algo, part)
		if err = d.run(t, args); err == nil {
			break
		}
		log.Printf("download copy %s (%s): %v", t.job.Key, enc, err)
		os.Remove(part)
		if enc == "libx264" || enc == "" || enc == "none" {
			break
		}
	}
	if err != nil {
		return 0, fmt.Errorf("could not convert the file")
	}
	if err := os.Rename(part, out); err != nil {
		return 0, err
	}
	st, err := os.Stat(out)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func (d *Downloads) run(t downloadTask, args []string) error {
	cmd := exec.Command(d.ffmpeg, args...)
	media.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	// -progress writes key=value lines; out_time_us is how far it is.
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		v, ok := strings.CutPrefix(sc.Text(), "out_time_us=")
		if !ok || t.file.Duration <= 0 {
			continue
		}
		if us, err := strconv.ParseFloat(v, 64); err == nil {
			p := min(0.99, max(0, us/1e6/t.file.Duration))
			d.set(t.job, func(j *DownloadJob) { j.Progress = p })
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// DownloadArgs are ffmpeg's arguments for a copy of f written to out.
func DownloadArgs(f *store.MediaFile, o DownloadOptions, encoder, device string, tonemap bool, tonemapAlgo, out string) []string {
	gpuIndex := device != "" && !strings.HasPrefix(device, "/")
	renderNode := "/dev/dri/renderD128"
	if device != "" && !gpuIndex {
		renderNode = device
	}
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-progress", "pipe:1", "-nostats"}
	switch encoder {
	case "vaapi":
		args = append(args, "-vaapi_device", renderNode)
	case "qsv":
		if device != "" && !gpuIndex {
			args = append(args, "-qsv_device", renderNode)
		}
	}
	args = append(args, "-i", f.Path)

	burn := -1
	for _, st := range f.Streams {
		if o.BurnSubtitle >= 0 && st.Index == o.BurnSubtitle && st.Type == "subtitle" && st.External == "" && IsImageSubtitle(st.Codec) {
			burn = st.Index
		}
	}
	var pre, post []string
	if tonemap {
		pre = append(pre, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
			"tonemap=tonemap="+tonemapAlgo+":desat=0", "zscale=t=bt709:m=bt709:r=tv")
	}
	if o.Height > 0 && o.Height < f.Height {
		post = append(post, fmt.Sprintf("scale=-2:%d", o.Height))
	}
	var codec []string
	switch encoder {
	case "nvenc":
		post = append(post, "format=yuv420p")
		codec = []string{"-c:v", "h264_nvenc", "-preset", "p5", "-profile:v", "high"}
		if gpuIndex {
			codec = append(codec, "-gpu", device)
		}
	case "qsv":
		post = append(post, "format=nv12")
		codec = []string{"-c:v", "h264_qsv", "-preset", "medium", "-profile:v", "high"}
	case "amf":
		post = append(post, "format=yuv420p")
		codec = []string{"-c:v", "h264_amf", "-quality", "balanced"}
	case "vaapi":
		post = append(post, "format=nv12", "hwupload")
		codec = []string{"-c:v", "h264_vaapi"}
	case "videotoolbox":
		post = append(post, "format=yuv420p")
		codec = []string{"-c:v", "h264_videotoolbox"}
	default:
		encoder = "libx264"
		post = append(post, "format=yuv420p")
		codec = []string{"-c:v", "libx264", "-preset", "fast", "-profile:v", "high"}
	}
	h := o.Height
	if h <= 0 {
		h = f.Height
	}
	kbps := downloadKbps(h)
	if burn >= 0 {
		args = append(args, "-filter_complex", fmt.Sprintf("[0:v:0]%s[base];[base][0:%d]overlay=eof_action=pass[ov];[ov]%s[vout]",
			chain(pre), burn, chain(post)), "-map", "[vout]")
	} else {
		args = append(args, "-map", "0:v:0", "-vf", strings.Join(append(pre, post...), ","))
	}
	audio := pickAudio(f.Streams, o.AudioStream)
	if audio != nil {
		args = append(args, "-map", fmt.Sprintf("0:%d", audio.Index))
	} else if len(f.Streams) == 0 {
		args = append(args, "-map", "0:a:0?")
	}
	args = append(args, codec...)
	args = append(args, "-maxrate", fmt.Sprintf("%dk", kbps), "-bufsize", fmt.Sprintf("%dk", kbps*2))
	if encoder == "libx264" {
		args = append(args, "-crf", "22")
	} else {
		args = append(args, "-b:v", fmt.Sprintf("%dk", kbps*3/4))
	}
	args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "160k",
		"-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1", "-movflags", "+faststart", "-f", "mp4", out)
	return args
}

// janitor deletes copies nobody has asked for in downloadKeep.
func (d *Downloads) janitor() {
	for {
		d.sweep(time.Now())
		time.Sleep(6 * time.Hour)
	}
}

func (d *Downloads) sweep(now time.Time) {
	files, _ := filepath.Glob(filepath.Join(d.dir, "*.mp4"))
	for _, p := range files {
		key := strings.TrimSuffix(filepath.Base(p), ".mp4")
		if strings.HasSuffix(key, ".part") {
			continue
		}
		d.mu.Lock()
		last, ok := d.used[key]
		if j := d.jobs[key]; j != nil && j.Status != "ready" && j.Status != "failed" {
			ok, last = true, now // being made
		}
		d.mu.Unlock()
		if !ok {
			if st, err := os.Stat(p); err == nil {
				last = st.ModTime()
			}
		}
		if now.Sub(last) > downloadKeep {
			os.Remove(p)
			d.mu.Lock()
			delete(d.jobs, key)
			delete(d.used, key)
			d.mu.Unlock()
		}
	}
}
