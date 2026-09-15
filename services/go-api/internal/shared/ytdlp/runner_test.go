package ytdlp

import (
	"context"
	"fmt"
	"testing"
)

// withBinary points DumpJSON at a stand-in executable for the duration of a
// test (yt-dlp is not installed in CI, and the binary name is otherwise fixed).
func withBinary(t *testing.T, name string) {
	t.Helper()
	prev := binaryName
	binaryName = name
	t.Cleanup(func() { binaryName = prev })
}

// Process-tree kill tests live in procgroup_unix_test.go.

// TestDumpJSON_CapsCapturedOutput reproduces the unbounded-buffer gap: stdout
// beyond the cap must not be buffered.
func TestDumpJSON_CapsCapturedOutput(t *testing.T) {
	withBinary(t, "sh")
	over := maxCaptureBytes + 4096

	lines, _, err := DumpJSON(context.Background(), []string{"-c", fmt.Sprintf("head -c %d /dev/zero", over)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	if total > maxCaptureBytes {
		t.Fatalf("stdout not capped: got %d bytes, cap %d", total, maxCaptureBytes)
	}
}
