package execcmd

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// Process-tree kill tests live in procgroup_unix_test.go.

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
