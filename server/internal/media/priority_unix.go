//go:build linux || darwin || freebsd || netbsd || openbsd

package media

import (
	"os/exec"
	"syscall"
)

// lowerPriority makes a started process nice 10, so it only gets the CPU
// that playback and the rest of the server leave over.
func lowerPriority(cmd *exec.Cmd) {
	if cmd.Process != nil {
		syscall.Setpriority(syscall.PRIO_PROCESS, cmd.Process.Pid, 10)
	}
}
