package ytdlp

import (
	"altune/go-api/internal/shared/execcmd"
	"context"
	"fmt"
	"strings"
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

// Exec, capture and process-tree kill are execcmd's; their tests live there.

// TestDumpJSON_CapsCapturedOutput reproduces the unbounded-buffer gap: stdout
// beyond the cap must not be buffered.
func TestDumpJSON_CapsCapturedOutput(t *testing.T) {
	withBinary(t, "sh")
	over := execcmd.MaxCaptureBytes + 4096

	lines, _, err := DumpJSON(context.Background(), []string{"-c", fmt.Sprintf("head -c %d /dev/zero", over)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	total := 0
	for _, l := range lines {
		total += len(l)
	}
	if total > execcmd.MaxCaptureBytes {
		t.Fatalf("stdout not capped: got %d bytes, cap %d", total, execcmd.MaxCaptureBytes)
	}
}

func TestDumpJSON_ReturnsOneMessagePerNonBlankLine(t *testing.T) {
	withBinary(t, "sh")

	lines, stderr, err := DumpJSON(context.Background(), []string{"-c", `printf '{"a":1}\n\n  \n{"b":2}\n'`})
	if err != nil {
		t.Fatalf("unexpected error: %v (stderr %q)", err, stderr)
	}
	got := make([]string, len(lines))
	for i, l := range lines {
		got[i] = string(l)
	}
	if want := []string{`{"a":1}`, `{"b":2}`}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines: got %v, want %v", got, want)
	}
}

func TestDumpJSON_FailureCarriesStderrAndTheRawError(t *testing.T) {
	withBinary(t, "sh")

	lines, stderr, err := DumpJSON(context.Background(), []string{"-c", `echo out; echo boom 1>&2; exit 3`})
	if err == nil {
		t.Fatal("expected an error for a non-zero exit, got nil")
	}
	if lines != nil {
		t.Fatalf("expected no lines on failure, got %v", lines)
	}
	if !strings.Contains(stderr, "boom") {
		t.Fatalf("stderr not returned: got %q", stderr)
	}
}

// TestDumpJSON_CancelledContextEndsTheRun: cancellation is the only deadline
// DumpJSON has, so the caller's ctx must still reach the process.
func TestDumpJSON_CancelledContextEndsTheRun(t *testing.T) {
	withBinary(t, "sh")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := DumpJSON(ctx, []string{"-c", "sleep 30"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancellation error, got nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DumpJSON did not return after its context was cancelled")
	}
}
