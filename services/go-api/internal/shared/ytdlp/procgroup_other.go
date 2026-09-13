//go:build !unix

package ytdlp

import "os/exec"

// setProcessGroup is a no-op on platforms without POSIX process groups; the
// default exec.CommandContext behaviour (kill the direct child) applies.
func setProcessGroup(cmd *exec.Cmd) {}
