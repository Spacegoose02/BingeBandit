//go:build linux

package media

import (
	"os/exec"
	"strings"
	"testing"
)

// Housekeeping jobs run niced, so playback gets the CPU first.
func TestRunBackgroundLowersPriority(t *testing.T) {
	// The shell waits a moment, so the priority is set before it reads it.
	out, err := RunBackground(exec.Command("sh", "-c", "sleep 0.2; cut -d' ' -f19 /proc/$$/stat"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "10" {
		t.Errorf("nice = %q, want 10", got)
	}
}
