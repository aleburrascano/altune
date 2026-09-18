// Package execcmd holds the shared CLI exec runner.
//
// It lives under internal/shared because the acquisition download adapters
// (chromaprint, streamrip, ytdlp) each duplicated the identical "run a CLI
// command, capture stdout+stderr" scaffolding, and internal/shared is the only
// place every feature may import without an import-direction violation.
// internal/shared/ytdlp builds its NDJSON runner on Run for the same reason.
// This package owns only the exec plumbing: no command names, no timeout
// policy, no error wording — those stay with each caller.
package execcmd

import (
	"context"
	"os/exec"
	"time"
)

// MaxCaptureBytes caps how much stdout (and, separately, stderr) is buffered
// in memory per invocation. Output beyond it is discarded, not stored, so
// callers that parse the capture may lose its tail rather than the process.
const MaxCaptureBytes = 8 << 20 // 8 MiB

// orphanWaitDelay bounds how long Wait keeps draining stdout/stderr after the
// direct child has exited (or been cancelled). Without it, a grandchild that
// inherited the pipes blocks Wait until the grandchild itself exits, ignoring
// the caller's deadline. On overrun Wait returns exec.ErrWaitDelay.
const orphanWaitDelay = 2 * time.Second

// Run runs `name args...` until it exits or ctx is done, capturing stdout and
// stderr separately. Cancelling ctx kills the whole process group, so a
// grandchild cannot outlive the call. It returns the captured stdout and
// stderr (both untrimmed) and the raw *exec.Cmd error, so callers keep their
// own error wording and wrapping chains intact.
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

// RunWithTimeout is Run under a context cancelled after timeout.
func RunWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return Run(cmdCtx, name, args...)
}
