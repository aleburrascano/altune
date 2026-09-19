package ytdlp

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestYtDlpAudioSearcher_Search(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed, skipping integration test")
	}

	searcher := NewYtDlpAudioSearcher("", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	candidates, err := searcher.Search(ctx, "The Weeknd Blinding Lights")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate, got 0")
	}

	first := candidates[0]
	if first.Title == "" {
		t.Error("first candidate has empty Title")
	}
	if first.URL == "" {
		t.Error("first candidate has empty URL")
	}
	if first.Duration <= 0 {
		t.Errorf("first candidate Duration = %v, want > 0", first.Duration)
	}
}

// withStubYtDlp puts a yt-dlp on PATH that prints the given stdout, so the
// search runs through the real DumpJSON exec and line scan.
func withStubYtDlp(t *testing.T, stdout string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'ALTUNE_EOF'\n" + stdout + "\nALTUNE_EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &logs
}

func TestRunYtDlpSearch_OutputThatParsesToNothingIsAnError(t *testing.T) {
	withStubYtDlp(t, "garbage\nmore garbage")
	s := NewYtDlpAudioSearcher("", "", "")

	_, err := s.runYtDlpSearch(context.Background(), "ytsearch5:q")

	if err == nil {
		t.Fatal("unparsable output reported as a successful empty search, want an error")
	}
	if !strings.Contains(err.Error(), "2 lines, 0 parsable") {
		t.Fatalf("error = %v, want it to carry the line and parsable counts", err)
	}
}

func TestRunYtDlpSearch_MixedOutputKeepsGoodEntriesAndLogsSkipped(t *testing.T) {
	withStubYtDlp(t, `{"title":"Good","webpage_url":"https://youtube.com/watch?v=1","duration":12}`+
		"\ngarbage\n"+`{"title":"No URL"}`)
	logs := captureLogs(t)
	s := NewYtDlpAudioSearcher("", "", "")

	got, err := s.runYtDlpSearch(context.Background(), "ytsearch5:q")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://youtube.com/watch?v=1" {
		t.Fatalf("candidates = %+v, want only the parsable entry with a URL", got)
	}
	logged := logs.String()
	if !strings.Contains(logged, "acquisition.search_lines_skipped") || !strings.Contains(logged, `"skipped":2`) {
		t.Fatalf("expected a warn line counting the 2 skipped lines, got:\n%s", logged)
	}
}

func TestRunYtDlpSearch_NoOutputIsAnEmptyResult(t *testing.T) {
	withStubYtDlp(t, "")
	s := NewYtDlpAudioSearcher("", "", "")

	got, err := s.runYtDlpSearch(context.Background(), "ytsearch5:q")
	if err != nil {
		t.Fatalf("a search that found nothing must not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("candidates = %+v, want none", got)
	}
}

func TestNewYtDlpAudioSearcher_ReturnsNonNil(t *testing.T) {
	searcher := NewYtDlpAudioSearcher("", "", "")
	if searcher == nil {
		t.Fatal("NewYtDlpAudioSearcher returned nil")
	}
}

func TestNewYtDlpAudioSearcher_StoresConfig(t *testing.T) {
	searcher := NewYtDlpAudioSearcher("/usr/bin/ffmpeg", "/tmp/cookies.txt", "deno")
	if searcher.ffmpegLocation != "/usr/bin/ffmpeg" {
		t.Errorf("ffmpegLocation = %q, want %q", searcher.ffmpegLocation, "/usr/bin/ffmpeg")
	}
	if searcher.cookieFile != "/tmp/cookies.txt" {
		t.Errorf("cookieFile = %q, want %q", searcher.cookieFile, "/tmp/cookies.txt")
	}
	if searcher.jsRuntime != "deno" {
		t.Errorf("jsRuntime = %q, want %q", searcher.jsRuntime, "deno")
	}
}
