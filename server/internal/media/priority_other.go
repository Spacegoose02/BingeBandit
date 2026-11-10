//go:build !(linux || darwin || freebsd || netbsd || openbsd || windows)

package media

import "os/exec"

func lowerPriority(cmd *exec.Cmd) {}
