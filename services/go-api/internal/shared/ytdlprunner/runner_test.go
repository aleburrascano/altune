package ytdlprunner_test

import (
	"altune/go-api/internal/shared/childprocess"
	"altune/go-api/internal/shared/ytdlprunner"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withShellAsYtDlp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("#!/bin/sh\nexec sh \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDumpJSON_KeepsLinesWhenOnlyStderrOverflows(t *testing.T) {
	withShellAsYtDlp(t)
	over := childprocess.MaxCaptureBytes + 4096

	lines, _, err := ytdlprunner.DumpJSON(context.Background(), []string{"-c", fmt.Sprintf(`printf '{"a":1}\n{"b":2}\n'; head -c %d /dev/zero 1>&2`, over)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
}

func TestDumpJSON_ReportsTruncatedOutputAndReturnsNoLines(t *testing.T) {
	withShellAsYtDlp(t)
	over := childprocess.MaxCaptureBytes + 4096

	lines, _, err := ytdlprunner.DumpJSON(context.Background(), []string{"-c", fmt.Sprintf("head -c %d /dev/zero | tr '\\0' 'a'", over)})
	if !errors.Is(err, childprocess.ErrOutputTruncated) {
		t.Fatalf("err = %v, want ErrOutputTruncated", err)
	}
	if lines != nil {
		t.Fatalf("expected no lines, got %d", len(lines))
	}
}

func TestDumpJSON_ReturnsOneMessagePerNonBlankLine(t *testing.T) {
	withShellAsYtDlp(t)

	lines, stderr, err := ytdlprunner.DumpJSON(context.Background(), []string{"-c", `printf '{"a":1}\n\n  \n{"b":2}\n'`})
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
	withShellAsYtDlp(t)

	lines, stderr, err := ytdlprunner.DumpJSON(context.Background(), []string{"-c", `echo out; echo boom 1>&2; exit 3`})
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
	withShellAsYtDlp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, _, err := ytdlprunner.DumpJSON(ctx, []string{"-c", "sleep 30"})
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
