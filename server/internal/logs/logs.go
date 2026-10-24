// Package logs captures the server log: a ring buffer for the admin Logs
// page plus a size-rotated file in the data folder.
package logs

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ringSize    = 3000
	maxFileSize = 10 << 20
	keepFiles   = 3
)

type Line struct {
	Seq   int64  `json:"seq"`
	Time  int64  `json:"time"` // unix millis
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Buffer is an io.Writer for the standard logger. Each Write is one line.
type Buffer struct {
	mu      sync.Mutex
	lines   []Line
	next    int64
	file    *os.File
	size    int64
	path    string
	console io.Writer
}

// New opens <dir>/bingebandit.log for appending and mirrors to console.
func New(dir string, console io.Writer) (*Buffer, error) {
	b := &Buffer{path: filepath.Join(dir, "bingebandit.log"), console: console}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := b.open(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Buffer) open() error {
	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	b.file, b.size = f, st.Size()
	return nil
}

// rotate shifts bingebandit.log → .1 → .2 …, dropping the oldest.
func (b *Buffer) rotate() {
	b.file.Close()
	for i := keepFiles - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", b.path, i), fmt.Sprintf("%s.%d", b.path, i+1))
	}
	os.Rename(b.path, b.path+".1")
	if err := b.open(); err != nil {
		b.file = nil
	}
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.console != nil {
		b.console.Write(p)
	}
	if b.file != nil {
		n, _ := b.file.Write(p)
		b.size += int64(n)
		if b.size > maxFileSize {
			b.rotate()
		}
	}
	for _, raw := range bytes.Split(bytes.TrimRight(p, "\n"), []byte("\n")) {
		text := string(raw)
		b.next++
		b.lines = append(b.lines, Line{Seq: b.next, Time: time.Now().UnixMilli(), Level: levelOf(text), Text: text})
	}
	if over := len(b.lines) - ringSize; over > 0 {
		b.lines = append(b.lines[:0:0], b.lines[over:]...)
	}
	return len(p), nil
}

// levelOf guesses a severity from the message, since the std logger has none.
func levelOf(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "panic") || strings.Contains(t, "fatal"):
		return "error"
	case strings.Contains(t, "error") || strings.Contains(t, "failed") || strings.Contains(t, " 500 ") || strings.Contains(t, " 502 "):
		return "error"
	case strings.Contains(t, "warn") || strings.Contains(t, "unavailable") || strings.Contains(t, "no tmdb match") || strings.Contains(t, "cannot"):
		return "warn"
	}
	return "info"
}

// Since returns buffered lines with Seq > after (all when after is 0).
func (b *Buffer) Since(after int64) []Line {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Line{}
	for _, l := range b.lines {
		if l.Seq > after {
			out = append(out, l)
		}
	}
	return out
}

// Path is the current log file, for downloads.
func (b *Buffer) Path() string { return b.path }
