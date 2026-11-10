package media

import (
	"bytes"
	"os/exec"
)

// RunBackground runs a housekeeping job (thumbnails and the like) at low
// priority and returns its combined output, like cmd.CombinedOutput.
func RunBackground(cmd *exec.Cmd) ([]byte, error) {
	hideWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	lowerPriority(cmd)
	err := cmd.Wait()
	return out.Bytes(), err
}
