//go:build !windows

package media

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

// HideWindow is exported for other packages that launch ffmpeg.
func HideWindow(cmd *exec.Cmd) {}
