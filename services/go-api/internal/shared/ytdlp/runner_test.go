package ytdlp

import (
	"altune/go-api/internal/shared/execcmd"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func withBinary(t *testing.T, name string) {
	t.Helper()
	prev := binaryName
	binaryName = name
	t.Cleanup(func() { binaryName = prev })
}

func TestDumpJSON_KeepsLinesWhenOnlyStderrOverflows(t *testing.T) {
	withBinary(t, "sh")
	over := execcmd.MaxCaptureBytes + 4096

	lines, _, err := DumpJSON(context.Background(), []string{"-c", fmt.Sprintf(`printf '{"a":1}\n{"b":2}\n'; head -c %d /dev/zero 1>&2`, over)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
}

func TestDumpJSON_ReportsTruncatedOutputAndReturnsNoLines(t *testing.T) {
	withBinary(t, "sh")
	over := execcmd.MaxCaptureBytes + 4096

	lines, _, err := DumpJSON(context.Background(), []string{"-c", fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'a'", over)})
	if !errors.Is(err, execcmd.ErrOutputTruncated) {
		t.Fatalf("err = %v, want ErrOutputTruncated", err)
	}
	if lines != nil {
		t.Fatalf("expected no lines, got %d", len(lines))
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
