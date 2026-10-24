//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

// hideWindow stops ffmpeg/ffprobe from flashing a console window when the
// server runs as a background app on Windows.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// HideWindow is exported for other packages that launch ffmpeg.
func HideWindow(cmd *exec.Cmd) { hideWindow(cmd) }
