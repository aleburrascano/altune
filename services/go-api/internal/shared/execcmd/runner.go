package execcmd

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

const MaxCaptureBytes = 8 << 20

var ErrOutputTruncated = errors.New("execcmd: captured output exceeded the cap")

const orphanWaitDelay = 2 * time.Second

func Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error) {
	stdout, stderr, _, err = RunCapture(ctx, name, args...)
	return stdout, stderr, err
}

func RunCapture(ctx context.Context, name string, args ...string) (stdout, stderr string, stdoutTruncated bool, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	setProcessGroup(cmd)
	cmd.WaitDelay = orphanWaitDelay
	stdoutBuf := &capWriter{limit: MaxCaptureBytes}
	stderrBuf := &capWriter{limit: MaxCaptureBytes}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err = cmd.Run()
	killProcessGroup(cmd)
	return stdoutBuf.String(), stderrBuf.String(), stdoutBuf.dropped, err
}

func RunWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return Run(cmdCtx, name, args...)
}

type capWriter struct {
	buf     bytes.Buffer
	limit   int
	dropped bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.limit - w.buf.Len(); room > 0 {
		if len(p) <= room {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:room])
			w.dropped = true
		}
	} else if len(p) > 0 {
		w.dropped = true
	}
	return len(p), nil
}

func (w *capWriter) String() string { return w.buf.String() }
