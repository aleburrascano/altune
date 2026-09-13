package execcmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestRunWithTimeout_KillsGrandchildProcessTree reproduces the gap where a
// timeout only kills the direct child: an outer shell (the child) backgrounds
// an inner shell (a grandchild, standing in for ffmpeg) that sleeps and then
// writes a marker. If only the child is killed, the grandchild survives the
// context cancellation and writes the marker after the deadline.
func TestRunWithTimeout_KillsGrandchildProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group kill semantics are POSIX-specific")
	}
	marker := filepath.Join(t.TempDir(), "grandchild.marker")
	script := fmt.Sprintf(`sh -c 'sleep 3; touch %q' & sleep 30`, marker)

	_, _, err := RunWithTimeout(context.Background(), 200*time.Millisecond, "sh", "-c", script)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}

	time.Sleep(4 * time.Second) // outlive the grandchild's own sleep
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("grandchild survived context cancellation and wrote %s", marker)
	}
}

// TestRunWithTimeout_CapsCapturedOutput reproduces the unbounded-buffer gap: a
// command that emits more than the cap must not grow the capture past it.
func TestRunWithTimeout_CapsCapturedOutput(t *testing.T) {
	over := maxCaptureBytes + 4096
	stdout, _, err := RunWithTimeout(
		context.Background(), 30*time.Second,
		"sh", "-c", fmt.Sprintf("head -c %d /dev/zero", over),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stdout) > maxCaptureBytes {
		t.Fatalf("stdout not capped: got %d bytes, cap %d", len(stdout), maxCaptureBytes)
	}
}
