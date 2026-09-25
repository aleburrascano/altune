package service

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// secretCookiePath is where an operator mounts the yt-dlp cookie jar; it must
// never reach log output (ARCHITECTURE §2.7).
const secretCookiePath = "/run/secrets/altune/yt_cookies.txt"

// ytdlpCookieErr mirrors the chain ytdlp.Download builds: the exec error with
// yt-dlp's stderr embedded verbatim, which names the --cookies file.
func ytdlpCookieErr() error {
	stderr := "ERROR: '" + secretCookiePath + "' does not look like a Netscape format cookies file " +
		"(called with --cookies " + secretCookiePath + ")"
	return fmt.Errorf("yt-dlp download: %w (stderr: %s)", errors.New("exit status 1"), stderr)
}

func captureDefaultLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func assertNoCookiePath(t *testing.T, logs string, wantMsg string) {
	t.Helper()
	if !strings.Contains(logs, wantMsg) {
		t.Fatalf("expected log %q, got:\n%s", wantMsg, logs)
	}
	if strings.Contains(logs, secretCookiePath) || strings.Contains(logs, "/run/secrets") {
		t.Fatalf("cookie file path leaked into logs:\n%s", logs)
	}
	if !strings.Contains(logs, "exit status 1") {
		t.Fatalf("redaction dropped the diagnostic error text:\n%s", logs)
	}
}

func TestRunPipeline_StepFailedLogRedactsCookiePath(t *testing.T) {
	logs := captureDefaultLog(t)
	step := &mockStep{name: "download", executeErr: ytdlpCookieErr()}

	err := RunPipeline(context.Background(), pipelineOf(step), &AcquisitionContext{})
	if err == nil {
		t.Fatal("expected pipeline error")
	}

	assertNoCookiePath(t, logs.String(), "pipeline step failed")
}

func TestRunPipeline_RollbackFailedLogRedactsCookiePath(t *testing.T) {
	logs := captureDefaultLog(t)
	first := &mockStep{name: "search", rollbackErr: ytdlpCookieErr()}
	second := &mockStep{name: "download", executeErr: errors.New("boom")}

	_ = RunPipeline(context.Background(), pipelineOf(first, second), &AcquisitionContext{})

	assertNoCookiePath(t, logs.String(), "rollback failed")
}

func TestDownloadStep_CandidateDownloadFailedLogRedactsCookiePath(t *testing.T) {
	logs := captureDefaultLog(t)
	step := NewDownloadStep(&fileWritingSearcher{err: ytdlpCookieErr()})
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x", Source: "youtube"}}}

	if _, err := step.Execute(context.Background(), ac, afterSelect{}); err == nil {
		t.Fatal("expected download error")
	}

	out := logs.String()
	assertNoCookiePath(t, out, "acquisition.candidate_download_failed")
	if !strings.Contains(out, "https://example.com/x") {
		t.Fatalf("candidate URL should stay in the log for triage:\n%s", out)
	}
}
