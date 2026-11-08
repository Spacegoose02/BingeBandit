// Package whisper makes subtitles from a video's own sound with whisper.cpp
// (github.com/ggml-org/whisper.cpp), for files that have none and that
// OpenSubtitles doesn't know. It runs on the server's own CPU (or GPU, if
// whisper.cpp was built for one); nothing leaves the server.
//
// whisper.cpp's command-line program comes from BB_WHISPER, or whisper-cli
// (or whisper-cpp) on the PATH. The model is the whisper_model setting: a
// model name ("base", "small", "large-v3-turbo"...), downloaded once from
// whisper.cpp's Hugging Face page into the data folder, or the path of a
// ggml model file already on the server.
package whisper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"bingebandit/internal/media"
)

// Models are the model names offered, smallest (fastest) first.
var Models = []string{"tiny", "base", "small", "medium", "large-v3-turbo"}

// ModelBase is where models are downloaded from; a variable so tests can point it elsewhere.
var ModelBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"

// ErrNoWhisper is returned when whisper.cpp isn't installed.
var ErrNoWhisper = errors.New("whisper.cpp isn't installed on the server: install it (whisper-cli on the PATH) or set BB_WHISPER to its program")

// Binary finds whisper.cpp's command-line program, or "".
func Binary() string {
	if p := os.Getenv("BB_WHISPER"); p != "" {
		return p
	}
	for _, name := range []string{"whisper-cli", "whisper-cpp"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// Available reports whether subtitles can be made here.
func Available() bool { return Binary() != "" }

// Request is one file to make subtitles for.
type Request struct {
	Path      string
	Stream    int    // audio stream index among the file's audio streams (0: the first)
	Language  string // two-letter code, or "" to detect it
	Translate bool   // write English subtitles whatever the language spoken
	Duration  float64
}

// Result is the subtitles made, as SRT, and the language they're in.
type Result struct {
	SRT      []byte
	Language string
}

// Runner makes subtitles, one file at a time.
type Runner struct {
	FFmpeg  string
	DataDir string
	Model   func() string // the whisper_model setting
	mu      sync.Mutex
	dlMu    sync.Mutex
}

var (
	progressRe = regexp.MustCompile(`progress\s*=\s*(\d+)%`)
	detectedRe = regexp.MustCompile(`auto-detected language:\s*([a-z]{2,3})`)
	modelName  = regexp.MustCompile(`^[a-z0-9.\-]+$`)
)

// modelPath is the model file to use, downloading it the first time.
func (r *Runner) modelPath(ctx context.Context, progress func(string, float64)) (string, error) {
	m := strings.TrimSpace(r.Model())
	if m == "" {
		m = "base"
	}
	if filepath.IsAbs(m) {
		if _, err := os.Stat(m); err != nil {
			return "", fmt.Errorf("the Whisper model file %s isn't there", m)
		}
		return m, nil
	}
	if !modelName.MatchString(m) {
		return "", fmt.Errorf("%q isn't a Whisper model name", m)
	}
	dir := filepath.Join(r.DataDir, "whisper")
	path := filepath.Join(dir, "ggml-"+m+".bin")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	r.dlMu.Lock()
	defer r.dlMu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	progress("Downloading the "+m+" model (once)", 0)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelBase+"/ggml-"+m+".bin", nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: time.Hour}).Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading the Whisper model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading the Whisper model: %s", resp.Status)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, &countingReader{r: resp.Body, total: resp.ContentLength, report: func(p float64) { progress("Downloading the "+m+" model (once)", p) }})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("downloading the Whisper model: %w", err)
	}
	return path, os.Rename(tmp, path)
}

type countingReader struct {
	r      io.Reader
	n      int64
	total  int64
	report func(float64)
	last   time.Time
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.total > 0 && time.Since(c.last) > time.Second {
		c.last = time.Now()
		c.report(float64(c.n) / float64(c.total))
	}
	return n, err
}

// Run makes subtitles for a file. progress gets what's happening and how
// far along it is (0..1).
func (r *Runner) Run(ctx context.Context, req Request, progress func(step string, done float64)) (*Result, error) {
	bin := Binary()
	if bin == "" {
		return nil, ErrNoWhisper
	}
	model, err := r.modelPath(ctx, progress)
	if err != nil {
		return nil, err
	}
	r.mu.Lock() // one at a time: whisper uses every core it gets
	defer r.mu.Unlock()

	dir, err := os.MkdirTemp(r.DataDir, "whisper-")
	if err != nil {
		if dir, err = os.MkdirTemp("", "whisper-"); err != nil {
			return nil, err
		}
	}
	defer os.RemoveAll(dir)

	// whisper.cpp reads 16 kHz mono WAV.
	progress("Reading the sound", 0)
	wav := filepath.Join(dir, "audio.wav")
	ffmpeg := r.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-i", req.Path, "-map", fmt.Sprintf("0:a:%d", max(req.Stream, 0)),
		"-vn", "-sn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-y", wav)
	media.HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("reading the sound: %v %s", err, strings.TrimSpace(string(out)))
	}

	progress("Listening", 0)
	lang := req.Language
	if lang == "" {
		lang = "auto"
	}
	out := filepath.Join(dir, "subs")
	args := []string{"-m", model, "-f", wav, "-l", lang, "-osrt", "-of", out, "-pp"}
	if req.Translate {
		args = append(args, "-tr")
	}
	cmd = exec.CommandContext(ctx, bin, args...)
	media.HideWindow(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	detected := ""
	var tail []string
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		line := sc.Text()
		if m := progressRe.FindStringSubmatch(line); m != nil {
			p, _ := strconv.Atoi(m[1])
			progress("Listening", float64(p)/100)
		}
		if m := detectedRe.FindStringSubmatch(line); m != nil {
			detected = m[1]
		}
		if len(tail) > 4 {
			tail = tail[1:]
		}
		tail = append(tail, line)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("whisper.cpp: %v %s", err, strings.Join(tail, " "))
	}
	srt, err := os.ReadFile(out + ".srt")
	if err != nil {
		return nil, fmt.Errorf("whisper.cpp wrote no subtitles: %s", strings.Join(tail, " "))
	}
	if len(strings.TrimSpace(string(srt))) == 0 {
		return nil, errors.New("no speech was heard in this file")
	}
	res := &Result{SRT: srt, Language: req.Language}
	switch {
	case req.Translate:
		res.Language = "en"
	case res.Language == "":
		res.Language = detected
	}
	if res.Language == "" || res.Language == "auto" {
		res.Language = "und"
	}
	return res, nil
}
