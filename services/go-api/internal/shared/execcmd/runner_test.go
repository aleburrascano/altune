package execcmd

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRunWithTimeout_CapsCapturedOutput(t *testing.T) {
	over := MaxCaptureBytes + 4096
	stdout, _, err := RunWithTimeout(
		context.Background(), 30*time.Second,
		"sh", "-c", fmt.Sprintf("head -c %d /dev/zero", over),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stdout) > MaxCaptureBytes {
		t.Fatalf("stdout not capped: got %d bytes, cap %d", len(stdout), MaxCaptureBytes)
	}
}
