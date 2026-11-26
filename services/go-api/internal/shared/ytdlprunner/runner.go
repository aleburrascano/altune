package ytdlprunner

import (
	"altune/go-api/internal/shared/childprocess"
	"context"
	"strings"
)

const binaryName = "yt-dlp"

func DumpJSON(ctx context.Context, args []string) (lines [][]byte, stderr string, err error) {
	stdout, capturedStderr, stdoutTruncated, runErr := childprocess.RunCapture(ctx, binaryName, args...)
	if runErr != nil {
		return nil, capturedStderr, runErr
	}
	if stdoutTruncated {
		return nil, capturedStderr, childprocess.ErrOutputTruncated
	}
	return nonEmptyLines(stdout), "", nil
}

func nonEmptyLines(stdout string) [][]byte {
	var lines [][]byte
	for _, line := range strings.Split(stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, []byte(line))
		}
	}
	return lines
}
