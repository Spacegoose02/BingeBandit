//go:build windows

package media

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// lowerPriority drops a started process to below-normal priority, so it
// only gets the CPU that playback and the rest of the server leave over.
func lowerPriority(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.SetPriorityClass(h, windows.BELOW_NORMAL_PRIORITY_CLASS)
}
