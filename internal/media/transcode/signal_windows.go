//go:build windows

package transcode

import "os/exec"

// Windows has no SIGTERM. os.Process.Signal(syscall.SIGTERM) returns
// "not supported by windows", so Kill is the only option here.
//
// Only affects local development on a Windows laptop: the transcoder ships in a
// debian-slim image where the Unix build tag applies and graceful shutdown
// works properly.
func signalTerminate(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
