package ytdlp

import (
	"altune/go-api/internal/shared/execcmd"
	"context"
	"strings"
)

var binaryName = "yt-dlp"

func DumpJSON(ctx context.Context, args []string) (lines [][]byte, stderr string, err error) {
	stdout, capturedStderr, runErr := execcmd.Run(ctx, binaryName, args...)
	if runErr != nil {
		return nil, capturedStderr, runErr
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
