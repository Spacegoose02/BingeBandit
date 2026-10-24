//go:build windows

package stream

import (
	"os"
	"syscall"
)

// OpenShared opens a file without blocking renames or deletes by other
// processes. ffmpeg replaces index.m3u8 by renaming a temp file over it, and
// Go's os.Open omits FILE_SHARE_DELETE, which makes that rename fail and
// freezes the playlist while we are reading it.
func OpenShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
