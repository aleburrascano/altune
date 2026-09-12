// Package execcmd holds the shared CLI exec-with-timeout runner.
//
// It lives under internal/shared because the acquisition download adapters
// (chromaprint, streamrip, ytdlp) each duplicated the identical "run a CLI
// command under a timeout, capture stdout+stderr" scaffolding, and
// internal/shared is the only place every feature may import without an
// import-direction violation. This package owns only the exec plumbing: no
// command names, no timeout policy, no error wording — those stay with each
// caller.
package execcmd

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// RunWithTimeout runs `name args...` under a context that is cancelled after
// timeout, capturing stdout and stderr separately. It returns the captured
// stdout and stderr (both untrimmed) and the raw *exec.Cmd error, so callers
// keep their own error wording and wrapping chains intact.
func RunWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) (stdout, stderr string, err error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, name, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}
