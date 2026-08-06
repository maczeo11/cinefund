//go:build windows

package transcode

import "os/exec"

// Windows has no SIGTERM. os.Process.Signal(syscall.SIGTERM) returns
// "not supported by windows", so Kill is the only option here.
//
// This only affects local development on a Windows laptop: the transcoder ships
// in the debian-slim image from docs/09 §11, where the Unix build tag applies
// and graceful shutdown works properly. Worth knowing rather than being
// surprised by a temp directory of truncated segments after a local Ctrl-C.
func signalTerminate(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
