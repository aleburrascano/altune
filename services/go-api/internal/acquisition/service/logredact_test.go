package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"altune/go-api/internal/acquisition/ports"
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

	err := RunPipeline(context.Background(), []Step{step}, &AcquisitionContext{})
	if err == nil {
		t.Fatal("expected pipeline error")
	}

	assertNoCookiePath(t, logs.String(), "pipeline step failed")
}

func TestRunPipeline_RollbackFailedLogRedactsCookiePath(t *testing.T) {
	logs := captureDefaultLog(t)
	first := &mockStep{name: "search", rollbackErr: ytdlpCookieErr()}
	second := &mockStep{name: "download", executeErr: errors.New("boom")}

	_ = RunPipeline(context.Background(), []Step{first, second}, &AcquisitionContext{})

	assertNoCookiePath(t, logs.String(), "rollback failed")
}

func TestDownloadStep_CandidateDownloadFailedLogRedactsCookiePath(t *testing.T) {
	logs := captureDefaultLog(t)
	step := NewDownloadStep(&fileWritingSearcher{err: ytdlpCookieErr()})
	ac := &AcquisitionContext{Ranked: []ports.AudioCandidate{{URL: "https://example.com/x", Source: "youtube"}}}

	if err := step.Execute(context.Background(), ac); err == nil {
		t.Fatal("expected download error")
	}

	out := logs.String()
	assertNoCookiePath(t, out, "acquisition.candidate_download_failed")
	if !strings.Contains(out, "https://example.com/x") {
		t.Fatalf("candidate URL should stay in the log for triage:\n%s", out)
	}
}

func TestLogSafeText(t *testing.T) {
	tmpFile := filepath.Join(os.TempDir(), "altune-acquire-123", "track.mp3")
	tests := []struct {
		name     string
		in       string
		wantGone []string
		wantKept []string
	}{
		{
			name:     "spaced cookies flag value",
			in:       "called with --cookies /etc/altune/jar",
			wantGone: []string{"/etc/altune/jar"},
			wantKept: []string{"--cookies [REDACTED]"},
		},
		{
			name:     "equals cookies flag value",
			in:       "argv: --cookies=/srv/jar.txt -f bestaudio",
			wantGone: []string{"/srv/jar.txt"},
			wantKept: []string{"--cookies=[REDACTED]", "-f bestaudio"},
		},
		{
			name:     "relative cookie file name",
			in:       "no such file: 'cookies.txt'",
			wantGone: []string{"cookies.txt"},
		},
		{
			name:     "python errno path without cookie in name",
			in:       "ERROR: [Errno 2] No such file or directory: '/home/deploy/secrets/yt'",
			wantGone: []string{"/home/deploy", "secrets/yt"},
			wantKept: []string{"[Errno 2] No such file or directory"},
		},
		{
			name:     "windows cookie path",
			in:       `open C:\Users\ops\cookies.txt: denied`,
			wantGone: []string{`C:\Users`},
		},
		{
			name:     "yt-dlp sign-in hint keeps its guidance and faq url",
			in:       "Sign in to confirm. Use --cookies-from-browser or --cookies for the authentication. See https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp",
			wantKept: []string{"--cookies for the authentication", "https://github.com/yt-dlp/yt-dlp/wiki/FAQ#how-do-i-pass-cookies-to-yt-dlp"},
		},
		{
			name:     "url secrets scrubbed like httptrace",
			in:       `Get "https://api.example.com/v1?api_key=abc123&q=x": timeout`,
			wantGone: []string{"abc123"},
			wantKept: []string{"api_key=REDACTED", "q=x"},
		},
		{
			name:     "temp scratch path kept for triage",
			in:       "no mp3 file produced in " + tmpFile,
			wantKept: []string{tmpFile},
		},
		{
			name:     "python attribute access is not a cookie file",
			in:       "    self.cookiejar.save()",
			wantKept: []string{"self.cookiejar.save()"},
		},
		{
			name:     "path glued to a key with equals",
			in:       "cookiefile=/run/secrets/yt loaded",
			wantGone: []string{"/run/secrets"},
		},
		{
			name:     "file url",
			in:       "cannot load file:///srv/private/jar.txt",
			wantGone: []string{"/srv/private"},
		},
		{
			name:     "quoted path with spaces",
			in:       `open "/Volumes/ops share/secret dir/yt.txt": denied`,
			wantGone: []string{"/Volumes", "secret dir/yt.txt"},
		},
		{
			name:     "temp dir traversal is not exempt",
			in:       "open " + os.TempDir() + "/../etc/shadow",
			wantGone: []string{"etc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := logSafeText(tt.in)
			for _, gone := range tt.wantGone {
				if strings.Contains(got, gone) {
					t.Errorf("logSafeText(%q) = %q, still contains %q", tt.in, got, gone)
				}
			}
			for _, kept := range tt.wantKept {
				if !strings.Contains(got, kept) {
					t.Errorf("logSafeText(%q) = %q, lost %q", tt.in, got, kept)
				}
			}
		})
	}
}

func TestLogSafeError_Nil(t *testing.T) {
	if got := logSafeError(nil); got != "" {
		t.Fatalf("logSafeError(nil) = %q, want empty", got)
	}
}
