//go:build unix

package ytdlp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts cmd in its own process group and makes context
// cancellation kill the whole group. Without this a timeout only signals
// yt-dlp itself, leaving a grandchild (e.g. ffmpeg) running past the deadline.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid targets the whole process group (== the child's pid,
		// since Setpgid made the child a group leader).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
