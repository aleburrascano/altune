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

// killProcessGroup SIGKILLs whatever is left of cmd's process group once Wait
// has returned. Cancel only fires while yt-dlp itself is still running, so a
// yt-dlp that exits on its own (crash, OOM kill) would otherwise orphan its
// ffmpeg. The group id cannot be recycled while any member survives, so this
// only ever reaches yt-dlp's own descendants; ESRCH (none left) is the normal
// case and is ignored.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
