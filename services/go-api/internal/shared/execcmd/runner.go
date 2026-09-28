package execcmd

import (
	"context"
	"os/exec"
	"time"
)

const MaxCaptureBytes = 8 << 20

const orphanWaitDelay = 2 * time.Second

func Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	setProcessGroup(cmd)
	cmd.WaitDelay = orphanWaitDelay
	stdoutBuf := &capWriter{limit: MaxCaptureBytes}
	stderrBuf := &capWriter{limit: MaxCaptureBytes}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err = cmd.Run()
	killProcessGroup(cmd)
	return stdoutBuf.String(), stderrBuf.String(), err
}

func RunWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return Run(cmdCtx, name, args...)
}
