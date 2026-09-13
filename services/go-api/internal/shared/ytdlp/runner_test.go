package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// withBinary points DumpJSON at a stand-in executable for the duration of a
// test (yt-dlp is not installed in CI, and the binary name is otherwise fixed).
func withBinary(t *testing.T, name string) {
	t.Helper()
	prev := binaryName
	binaryName = name
	t.Cleanup(func() { binaryName = prev })
}

// TestDumpJSON_KillsGrandchildProcessTree reproduces the gap where a timeout
// only kills the direct child (yt-dlp) while a grandchild (ffmpeg) keeps
// running past the deadline.
func TestDumpJSON_KillsGrandchildProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill semantics are POSIX-specific")
	}
	withBinary(t, "sh")
	marker := filepath.Join(t.TempDir(), "grandchild.marker")
	script := fmt.Sprintf(`sh -c 'sleep 3; touch %q' & sleep 30`, marker)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, _, err := DumpJSON(ctx, []string{"-c", script}); err == nil {
		t.Fatal("expected a timeout error, got nil")
	}

	time.Sleep(4 * time.Second) // outlive the grandchild's own sleep
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("grandchild survived context cancellation and wrote %s", marker)
	}
}

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
