// Package ytdlp holds the shared low-level yt-dlp NDJSON runner.
//
// It lives under internal/shared because both the acquisition searcher and the
// discovery SoundCloud provider need the identical exec + line-scan loop, and
// internal/shared is the only place both features may import without an
// import-direction violation (a feature importing another feature is denied by
// the depguard boundaries in .golangci.yml). This package deliberately owns
// nothing feature-specific: no flags, no timeout policy, no entry->domain
// mapping — those stay with each caller.
package ytdlp

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// DumpJSON runs `yt-dlp <args...>` and returns each non-empty stdout line as a
// raw NDJSON message. It owns only the exec + NDJSON line scan that used to be
// duplicated in acquisition and discovery; callers keep their own flag list,
// timeout (via ctx), error wording, and entry->domain mapping.
//
// On exec failure it returns the raw *exec.Cmd error alongside the captured
// stderr (untrimmed) so callers can format their own messages and keep their
// error-wrapping chains intact. On success stderr is empty.
func DumpJSON(ctx context.Context, args []string) (lines [][]byte, stderr string, err error) {
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if runErr := cmd.Run(); runErr != nil {
		return nil, stderrBuf.String(), runErr
	}

	for _, line := range strings.Split(stdoutBuf.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, []byte(line))
		}
	}
	return lines, "", nil
}
