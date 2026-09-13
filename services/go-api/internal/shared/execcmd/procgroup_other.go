//go:build !unix

package execcmd

import "os/exec"

// setProcessGroup is a no-op on platforms without POSIX process groups; the
// default exec.CommandContext behaviour (kill the direct child) applies.
func setProcessGroup(cmd *exec.Cmd) {}
