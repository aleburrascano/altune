package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// secretCookiePath is where an operator mounts the yt-dlp cookie jar.
const secretCookiePath = "/secret/cookies.txt"

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

// withDecoyYtDlp puts a yt-dlp on PATH that fails and downloads nothing, so a
// Download that execs the bare name instead of the configured binary fails
// visibly rather than falling through to whatever the host has installed.
func withDecoyYtDlp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubDownloaderAt writes an executable standing in for an operator-configured
// yt-dlp path: it produces one mp3 over Download's minimum size in outDir.
func stubDownloaderAt(t *testing.T, outDir string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "custom-yt-dlp")
	script := "#!/bin/sh\nhead -c 20480 /dev/zero > " + filepath.Join(outDir, "stub.mp3") + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestYtDlpAudioSearcher_Download_RunsTheConfiguredBinary(t *testing.T) {
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = stubDownloaderAt(t, outDir)

	got, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir)
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}

	if want := filepath.Join(outDir, "stub.mp3"); got != want {
		t.Fatalf("Download = %q, want the file the configured binary produced (%q)", got, want)
	}
}

// argvRecordingDownloaderAt writes an executable that records its argv and then
// produces one mp3 over Download's minimum size, so a test can assert on the
// flags yt-dlp is actually invoked with.
func argvRecordingDownloaderAt(t *testing.T, outDir string) (binary, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	binary, argvFile = filepath.Join(dir, "recording-yt-dlp"), filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argvFile + "\"\n" +
		"head -c 20480 /dev/zero > \"" + filepath.Join(outDir, "stub.mp3") + "\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, argvFile
}

// Issue #1976: a three-hour set must be refused before its bytes are spent, so
// the size cap has to reach yt-dlp itself rather than be checked afterwards.
func TestYtDlpAudioSearcher_Download_CapsTheSourceFileSize(t *testing.T) {
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	binary, argvFile := argvRecordingDownloaderAt(t, outDir)
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary

	if _, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir); err != nil {
		t.Fatalf("Download error: %v", err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	for i, arg := range argv {
		if arg != "--max-filesize" {
			continue
		}
		if i+1 >= len(argv) || argv[i+1] != maxSourceFileSize {
			t.Fatalf("argv = %q, want --max-filesize followed by %q", argv, maxSourceFileSize)
		}
		return
	}
	t.Fatalf("argv = %q, want it to carry --max-filesize", argv)
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

// withFailingYtDlp puts a yt-dlp on PATH that exits 1 with the given stderr, so
// a test can drive the real exec path with the output a throttled or unreachable
// yt-dlp prints.
func withFailingYtDlp(t *testing.T, stderr string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho '" + stderr + "' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Issue #1983: yt-dlp exits 1 both for a query nothing matches and for a
// provider that refused to answer. Only the second is evidence about the
// source, and undistinguished it reaches the user as "couldn't find this track".
func TestRunYtDlpSearch_ClassifiesASourceThatRefusedToAnswer(t *testing.T) {
	tests := []struct {
		name            string
		stderr          string
		wantUnavailable bool
	}{
		{"throttled", "ERROR: HTTP Error 429: Too Many Requests", true},
		{"nothing reached youtube", "ERROR: unable to download: Temporary failure in name resolution", true},
		{"youtube answered and refused this video", "ERROR: [youtube] dQw4w9WgXcQ: Video unavailable", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFailingYtDlp(t, tt.stderr)
			s := NewYtDlpAudioSearcher("", "", "")

			_, err := s.runYtDlpSearch(context.Background(), "ytsearch5:q")

			if err == nil {
				t.Fatal("a yt-dlp that exited 1 reported no error")
			}
			if got := ports.IsSourceUnavailable(err); got != tt.wantUnavailable {
				t.Errorf("IsSourceUnavailable(%v) = %v, want %v", err, got, tt.wantUnavailable)
			}
		})
	}
}

// Issue #1983: a yt-dlp that is not installed is the source being unavailable,
// the one case where no output exists to classify on.
func TestYtDlpAudioSearcher_Download_MissingBinaryIsAnUnavailableSource(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp-absent")

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if !ports.IsSourceUnavailable(err) {
		t.Errorf("Download error = %v, want a missing yt-dlp to read as an unavailable source", err)
	}
}

// cookieJarError mirrors the chain runYtDlpSearch builds: the exec error with
// yt-dlp's stderr embedded verbatim, which names the --cookies file an operator
// mounted (ARCHITECTURE §2.7).
func cookieJarError() error {
	return fmt.Errorf("yt-dlp search: %w (stderr: ERROR: unable to open --cookies %s)",
		errors.New("exit status 1"), secretCookiePath)
}

// Issue #1973: the engine failure log carried the subprocess error verbatim,
// and the cookie jar path is a credential location the service-side log sites
// have masked all along.
func TestYtDlpAudioSearcher_Search_EngineFailureLogRedactsTheCookiePath(t *testing.T) {
	logs := captureLogs(t)
	s := withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, cookieJarError()
	})

	if _, err := s.Search(context.Background(), "q"); err == nil {
		t.Fatal("every engine failed, Search reported no error")
	}

	logged := logs.String()
	if !strings.Contains(logged, "acquisition.engine_search_failed") {
		t.Fatalf("expected the engine failure log, got:\n%s", logged)
	}
	if strings.Contains(logged, "/secret") {
		t.Fatalf("the cookie jar path leaked into the log:\n%s", logged)
	}
	if !strings.Contains(logged, "exit status 1") {
		t.Fatalf("redaction dropped the diagnostic text:\n%s", logged)
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

func TestYtDlpAudioSearcher_Available_MissingBinary(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp-absent")
	if s.Available() {
		t.Error("Available() = true for a missing yt-dlp binary, want false")
	}
}

func TestYtDlpAudioSearcher_Available_PresentBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = path
	if !s.Available() {
		t.Error("Available() = false for a present yt-dlp binary, want true")
	}
}

func withRunner(r searchRunner) *YtDlpAudioSearcher {
	s := NewYtDlpAudioSearcher("", "", "")
	s.runSearch = r
	return s
}

func TestYtDlpAudioSearcher_Search_QueriesBothEngines(t *testing.T) {
	var specs []string
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		specs = append(specs, spec)
		switch spec {
		case "ytsearch5:song artist":
			return []ports.AudioCandidate{{Title: "YT", URL: "https://youtube.com/watch?v=1"}}, nil
		case "scsearch5:song artist":
			return []ports.AudioCandidate{{Title: "SC", URL: "https://soundcloud.com/x/leak"}}, nil
		}
		return nil, nil
	})

	got, err := s.Search(context.Background(), "song artist")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if len(specs) != 2 || specs[0] != "ytsearch5:song artist" || specs[1] != "scsearch5:song artist" {
		t.Fatalf("engine specs = %v, want yt then sc", specs)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 merged candidates, got %d: %+v", len(got), got)
	}
}

func TestYtDlpAudioSearcher_Search_DedupsByURL(t *testing.T) {
	dup := ports.AudioCandidate{Title: "Dup", URL: "https://soundcloud.com/x/same"}
	s := withRunner(func(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
		return []ports.AudioCandidate{dup}, nil
	})

	got, err := s.Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected duplicate URL collapsed to 1, got %d", len(got))
	}
}

func TestYtDlpAudioSearcher_Search_OneEngineFails(t *testing.T) {
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		if spec == "ytsearch5:q" {
			return nil, errors.New("youtube blew up")
		}
		return []ports.AudioCandidate{{Title: "SC", URL: "https://soundcloud.com/x/leak"}}, nil
	})

	got, err := s.Search(context.Background(), "q")
	if err != nil {
		t.Fatalf("a single engine failure must not fail the search, got: %v", err)
	}
	if len(got) != 1 || got[0].Title != "SC" {
		t.Fatalf("expected the surviving engine's candidate, got %+v", got)
	}
}

func TestYtDlpAudioSearcher_Search_BothEnginesFail(t *testing.T) {
	s := withRunner(func(_ context.Context, _ string) ([]ports.AudioCandidate, error) {
		return nil, errors.New("down")
	})

	if _, err := s.Search(context.Background(), "q"); err == nil {
		t.Fatal("expected an error when every engine fails")
	}
}

func TestYtDlpAudioSearcher_Download_RejectsAnOversizeOutput(t *testing.T) {
	outDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "big-yt-dlp")
	script := "#!/bin/sh\ntruncate -s 210M \"" + filepath.Join(outDir, "big.mp3") + "\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("Download error = %v, want a too-large rejection", err)
	}
}

func hangingBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDownload_TimeoutUnderLiveParentIsSourceUnavailable(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = hangingBinary(t)
	s.downloadTimeout = 100 * time.Millisecond

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if !ports.IsSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a source-unavailable error", err)
	}
}

func TestDownload_ParentCancellationIsNotSourceUnavailable(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = hangingBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := s.Download(ctx, "https://youtube.com/watch?v=1", t.TempDir())

	if err == nil || ports.IsSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a plain error", err)
	}
}
