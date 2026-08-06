//go:build !windows

package transcode

import (
	"os/exec"
	"syscall"
)

// signalTerminate asks FFmpeg to stop cleanly. It finishes the current segment
// and closes its files, which is the difference between a temp directory full of
// valid segments and one full of truncated ones.
func signalTerminate(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}
