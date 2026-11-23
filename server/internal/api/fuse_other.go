//go:build !linux

package api

// OnFUSE reports whether path is on a FUSE filesystem. Only Linux servers
// (Docker, Unraid) are checked.
func OnFUSE(string) bool { return false }
