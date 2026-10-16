package ytdlp

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

const secretCookiePath = "/secret/cookies.txt"

func TestYtDlpAudioSearcher_Search(t *testing.T) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Skip("yt-dlp not installed, skipping integration test")
	}

	searcher := NewYtDlpAudioSearcher("", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	candidates, err := searcher.SearchQueries(ctx, []string{"The Weeknd Blinding Lights"})
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

func withStubYtDlp(t *testing.T, stdout string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat <<'ALTUNE_EOF'\n" + stdout + "\nALTUNE_EOF\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func withDecoyYtDlp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func stubDownloaderAt(t *testing.T, outDir string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "custom-yt-dlp")
	script := "#!/bin/sh\nhead -c 20480 /dev/zero > " + filepath.Join(outDir, "stub.m4a") + "\n" +
		"echo 'altune-output-path:" + filepath.Join(outDir, "stub.m4a") + "'\n"
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

	if want := filepath.Join(outDir, "stub.m4a"); got.Path != want {
		t.Fatalf("Download = %q, want the file the configured binary produced (%q)", got.Path, want)
	}
}

func argvRecordingDownloaderAt(t *testing.T, outDir string) (binary, argvFile string) {
	t.Helper()
	dir := t.TempDir()
	binary, argvFile = filepath.Join(dir, "recording-yt-dlp"), filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argvFile + "\"\n" +
		"head -c 20480 /dev/zero > \"" + filepath.Join(outDir, "stub.m4a") + "\"\n" +
		"echo 'altune-output-path:" + filepath.Join(outDir, "stub.m4a") + "'\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary, argvFile
}

func TestDownload_MaxFilesizeFlagIsTheDownloadedFileCap(t *testing.T) {
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
		if i+1 >= len(argv) || argv[i+1] != strconv.Itoa(maxDownloadedFileBytes) {
			t.Fatalf("argv = %q, want --max-filesize followed by %q", argv, strconv.Itoa(maxDownloadedFileBytes))
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

func withFailingYtDlp(t *testing.T, stderr string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho '" + stderr + "' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

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
			if got := acqdomain.IsSourceUnavailable(err); got != tt.wantUnavailable {
				t.Errorf("IsSourceUnavailable(%v) = %v, want %v", err, got, tt.wantUnavailable)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_MissingBinaryIsAnUnavailableSource(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp-absent")

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if !acqdomain.IsSourceUnavailable(err) {
		t.Errorf("Download error = %v, want a missing yt-dlp to read as an unavailable source", err)
	}
}

func cookieJarError() error {
	return fmt.Errorf("yt-dlp search: %w (stderr: ERROR: unable to open --cookies %s)",
		errors.New("exit status 1"), secretCookiePath)
}

func TestYtDlpAudioSearcher_Search_EngineFailureLogRedactsTheCookiePath(t *testing.T) {
	logs := captureLogs(t)
	s := withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, cookieJarError()
	})

	if _, err := s.SearchQueries(context.Background(), []string{"q"}); err == nil {
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
	var mu sync.Mutex
	var specs []string
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
		switch spec {
		case "ytsearch5:song artist":
			return []ports.AudioCandidate{{Title: "YT", URL: "https://youtube.com/watch?v=1"}}, nil
		case "scsearch5:song artist":
			return []ports.AudioCandidate{{Title: "SC", URL: "https://soundcloud.com/x/leak"}}, nil
		}
		return nil, nil
	})

	got, err := s.SearchQueries(context.Background(), []string{"song artist"})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	slices.Sort(specs)
	if !slices.Equal(specs, []string{"scsearch5:song artist", "ytsearch5:song artist"}) {
		t.Fatalf("engine specs = %v, want yt and sc", specs)
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

	got, err := s.SearchQueries(context.Background(), []string{"q"})
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

	got, err := s.SearchQueries(context.Background(), []string{"q"})
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

	if _, err := s.SearchQueries(context.Background(), []string{"q"}); err == nil {
		t.Fatal("expected an error when every engine fails")
	}
}

func TestYtDlpAudioSearcher_Download_RejectsAnOversizeOutput(t *testing.T) {
	outDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "big-yt-dlp")
	script := "#!/bin/sh\ntruncate -s 210M \"" + filepath.Join(outDir, "big.m4a") + "\"\n" +
		"echo 'altune-output-path:" + filepath.Join(outDir, "big.m4a") + "'\n"
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

func downloadWithScript(t *testing.T, body string) (ports.FetchedAudio, error) {
	t.Helper()
	outDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fake-yt-dlp")
	script := "#!/bin/sh\n" + strings.ReplaceAll(body, "OUT", outDir) + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary
	return s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir)
}

func TestDownload_UsesThePathYtDlpPrinted(t *testing.T) {
	t.Run("returns the printed file even when a larger stray m4a sits beside it", func(t *testing.T) {
		got, err := downloadWithScript(t, "truncate -s 20K 'OUT/Freewave 6.m4a'; truncate -s 90K OUT/stray.m4a; echo 'altune-output-path:OUT/Freewave 6.m4a'")
		if err != nil || filepath.Base(got.Path) != "Freewave 6.m4a" {
			t.Fatalf("Download = %q, %v, want Freewave 6.m4a", got.Path, err)
		}
	})
	t.Run("errors when no path is printed", func(t *testing.T) {
		_, err := downloadWithScript(t, "truncate -s 20K OUT/a.m4a")
		if err == nil || err.Error() != "no output path printed by yt-dlp" {
			t.Fatalf("Download error = %v, want no output path printed", err)
		}
	})
	t.Run("rejects a printed path outside the output dir", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "elsewhere.m4a")
		if err := os.WriteFile(outside, make([]byte, 20*1024), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := downloadWithScript(t, "echo 'altune-output-path:"+outside+"'")
		if err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("Download error = %v, want an outside-dir rejection", err)
		}
	})
	t.Run("rejects a printed path that is not an m4a", func(t *testing.T) {
		_, err := downloadWithScript(t, "truncate -s 20K OUT/a.mp3; echo 'altune-output-path:OUT/a.mp3'")
		if err == nil || !strings.Contains(err.Error(), "not an m4a") {
			t.Fatalf("Download error = %v, want a not-m4a rejection", err)
		}
	})
}

func TestParseOutputPath_ReadsRealYtDlpStdout(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ytdlp_2026.08.19_soundcloud_download_stdout.txt"))
	if err != nil {
		t.Fatal(err)
	}
	stdout := string(raw)
	if want := "/tmp/dl2 out/The Sound Of Silence (Simon & Garfunkel).m4a"; parseOutputPath(stdout) != want {
		t.Fatalf("parseOutputPath = %q, want %q", parseOutputPath(stdout), want)
	}
	if got, want := parseSourceFormat(stdout), (ports.SourceFormat{ID: "hls_aac_160k", Codec: "mp4a.40.2", Kbps: 160}); got != want {
		t.Fatalf("parseSourceFormat = %+v, want %+v", got, want)
	}
}

func TestYtDlpAudioSearcher_Download_RejectsWhenNothingIsProduced(t *testing.T) {
	_, err := downloadWithScript(t, "true")
	if err == nil || !strings.Contains(err.Error(), "no output path printed by yt-dlp") {
		t.Fatalf("Download error = %v, want a no-output-path rejection", err)
	}
}

func TestYtDlpAudioSearcher_Download_RejectsAnUndersizeOutput(t *testing.T) {
	_, err := downloadWithScript(t, "truncate -s 100 OUT/tiny.m4a; echo altune-output-path:OUT/tiny.m4a")
	want := "downloaded file too small (100 bytes), likely corrupt"
	if err == nil || err.Error() != want {
		t.Fatalf("Download error = %v, want %q", err, want)
	}
}

func TestYtDlpAudioSearcher_Download_ReturnsThePrintedFile(t *testing.T) {
	got, err := downloadWithScript(t, "truncate -s 20K OUT/small.m4a; truncate -s 40K OUT/large.m4a; echo altune-output-path:OUT/small.m4a")
	if err != nil || filepath.Base(got.Path) != "small.m4a" {
		t.Fatalf("Download = %q, %v, want the printed small.m4a, not the larger large.m4a", got.Path, err)
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

	if !acqdomain.IsSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a source-unavailable error", err)
	}
}

func TestDownload_ParentCancellationIsNotSourceUnavailable(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = hangingBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := s.Download(ctx, "https://youtube.com/watch?v=1", t.TempDir())

	if err == nil || acqdomain.IsSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a plain error", err)
	}
}

func formatArgRecorder(t *testing.T, outDir, stdout string) (binary, argsFile string) {
	t.Helper()
	argsFile = filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > \"" + argsFile + "\"\n"
	if outDir != "" {
		script += "truncate -s 1M \"" + filepath.Join(outDir, "out.m4a") + "\"\n"
		script += "echo 'altune-output-path:" + filepath.Join(outDir, "out.m4a") + "'\n"
	}
	if stdout != "" {
		script += "echo '" + stdout + "'\n"
	}
	return withYtDlpScript(t, script), argsFile
}

func formatFlag(t *testing.T, argsFile string) string {
	t.Helper()
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, a := range args {
		if a == "-f" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no -f flag in yt-dlp args %q", args)
	return ""
}

func TestDownload_FallsBackToACombinedFormatWhenNoAudioOnlyStreamExists(t *testing.T) {
	outDir := t.TempDir()
	s := NewYtDlpAudioSearcher("", "", "")
	var argsFile string
	s.binary, argsFile = formatArgRecorder(t, outDir, "")

	if _, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir); err != nil {
		t.Fatalf("Download() = %v, want nil", err)
	}

	got := formatFlag(t, argsFile)
	audioOnly, combined, ok := strings.Cut(got, "/best[")
	if !ok || !strings.HasSuffix(audioOnly, "/bestaudio") {
		t.Errorf("-f %q, want audio-only streams preferred before any combined format", got)
	}
	if !strings.HasSuffix(combined, "/best") {
		t.Errorf("-f %q, want a combined-format fallback after the audio-only streams", got)
	}
}

const liveJarContents = "# live cookie jar"

func jarWritingYtDlpAt(t *testing.T, path, outDir string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"prev=\"\"\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--cookies\" ]; then echo '# saved by yt-dlp' > \"$arg\" || exit 1; fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"echo '{\"title\":\"Song\",\"webpage_url\":\"https://www.youtube.com/watch?v=aaaaaaaaaaa\"}'\n" +
		"head -c 20480 /dev/zero > \"" + filepath.Join(outDir, "stub.m4a") + "\"\n" +
		"echo 'altune-output-path:" + filepath.Join(outDir, "stub.m4a") + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readOnlyLiveJar(t *testing.T) string {
	t.Helper()
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte(liveJarContents), 0o444); err != nil {
		t.Fatal(err)
	}
	return jar
}

func assertLiveJarUntouched(t *testing.T, jar string) {
	t.Helper()
	got, err := os.ReadFile(jar)
	if err != nil || string(got) != liveJarContents {
		t.Fatalf("live cookie jar = %q, err %v, want it untouched", got, err)
	}
}

func TestYtDlpAudioSearcher_Search_WorksAgainstAReadOnlyCookieJar(t *testing.T) {
	binDir := t.TempDir()
	jarWritingYtDlpAt(t, filepath.Join(binDir, "yt-dlp"), t.TempDir())
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	jar := readOnlyLiveJar(t)

	s := NewYtDlpAudioSearcher("", jar, "")
	got, err := s.runYtDlpSearch(context.Background(), "ytsearch5:song")
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("search returned %d candidates, want 1", len(got))
	}
	assertLiveJarUntouched(t, jar)
}

func TestYtDlpAudioSearcher_Download_WorksAgainstAReadOnlyCookieJar(t *testing.T) {
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	jar := readOnlyLiveJar(t)

	s := NewYtDlpAudioSearcher("", jar, "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp")
	jarWritingYtDlpAt(t, s.binary, outDir)

	if _, err := s.Download(context.Background(), "https://www.youtube.com/watch?v=aaaaaaaaaaa", outDir); err != nil {
		t.Fatalf("download error: %v", err)
	}
	assertLiveJarUntouched(t, jar)
}

func TestYtDlpAudioSearcher_SearchQueries_MergesEveryQueryAndEngine(t *testing.T) {
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		return []ports.AudioCandidate{{Title: spec, URL: "https://example.test/" + spec}}, nil
	})

	got, err := s.SearchQueries(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("SearchQueries error: %v", err)
	}
	if len(got) != len(searchEngines)*2 {
		t.Fatalf("merged candidates = %d, want %d (every query against every engine)", len(got), len(searchEngines)*2)
	}
}

func TestYtDlpAudioSearcher_SearchQueries_AllQueriesUnavailableIsSourceUnavailable(t *testing.T) {
	s := withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, &acqdomain.SourceUnavailableError{Source: SourceName, Err: errors.New("throttled")}
	})

	_, err := s.SearchQueries(context.Background(), []string{"a", "b"})

	if !acqdomain.IsSourceUnavailable(err) {
		t.Fatalf("SearchQueries error = %v, want a source-unavailable error when every query and engine is unavailable", err)
	}
}

func TestYtDlpAudioSearcher_DownloadPreview_RequestsOnlyTheLeadingSectionBeforeTheURL(t *testing.T) {
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	binary, argvFile := argvRecordingDownloaderAt(t, outDir)
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary

	if _, err := s.DownloadPreview(context.Background(), "https://youtube.com/watch?v=1", outDir, 130); err != nil {
		t.Fatalf("DownloadPreview error: %v", err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	sections, separator := slices.Index(argv, "--download-sections"), slices.Index(argv, "--")
	if sections < 0 || argv[sections+1] != "*0-130" || sections > separator {
		t.Fatalf("argv = %q, want --download-sections *0-130 before --", argv)
	}
}

func TestYtDlpAudioSearcher_MarkUnplayable_SetsTheExactPersistedReasonStrings(t *testing.T) {
	drm := inspectionFromInfo(inspectedInfo{Duration: 240, Formats: []inspectedFormat{
		{FormatID: "hls_aac_160k_encrypted", VCodec: "none", HasDRM: true},
	}})
	cases := []struct {
		name       string
		inspection inspection
		want       string
	}{
		{"drm", drm, "drm"},
		{"preview", inspection{Duration: 30}, "preview"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := withRunner(nil)
			s.inspect = staticInspection(tc.inspection)
			candidates := []ports.AudioCandidate{{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 240}}

			s.MarkUnplayable(context.Background(), candidates)

			if got := candidates[0].Unplayable; got != tc.want {
				t.Fatalf("Unplayable = %q, want %q", got, tc.want)
			}
		})
	}
}

const leakyStderrScript = "#!/bin/sh\n" +
	"echo \"ERROR: unable to open /home/op/secrets/cookies.txt https://cdn.example/a?Expires=1&token=SIGNEDTOKEN123456&Signature=SIGNEDSIG987654\" >&2\n" +
	"head -c 100000 /dev/zero | tr '\\0' 'x' >&2\n" +
	"exit 1\n"

func withLeakyYtDlp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(path, []byte(leakyStderrScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

func assertStderrEmbeddedSafely(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error from the failing binary")
	}
	msg := err.Error()
	if !strings.Contains(msg, "(stderr: ") {
		t.Fatalf("error lost its stderr section (binary not run?): %q", msg)
	}
	for _, leak := range []string{"cookies.txt", "SIGNEDTOKEN123456", "SIGNEDSIG987654"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error leaks %q: %.300s", leak, msg)
		}
	}
	if limit := maxEmbeddedStderrBytes + 512; len(msg) > limit {
		t.Errorf("error is %d bytes, want <= %d", len(msg), limit)
	}
}

func TestYtDlpAudioSearcher_SearchRedactsAndCapsStderr(t *testing.T) {
	withLeakyYtDlp(t)
	s := NewYtDlpAudioSearcher("", "", "")
	_, err := s.SearchQueries(context.Background(), []string{"q"})
	assertStderrEmbeddedSafely(t, err)
}

func TestYtDlpAudioSearcher_DownloadRedactsAndCapsStderr(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = withLeakyYtDlp(t)
	_, err := s.Download(context.Background(), "https://example.com/v", t.TempDir())
	assertStderrEmbeddedSafely(t, err)
}

func TestYtDlpAudioSearcher_InspectRedactsAndCapsStderr(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = withLeakyYtDlp(t)
	_, err := s.runYtDlpInspect(context.Background(), "https://soundcloud.com/a/b")
	assertStderrEmbeddedSafely(t, err)
}

func TestEmbeddedStderr_CapsOnRuneBoundaryWithSuffix(t *testing.T) {
	got := embeddedStderr(strings.Repeat("é", maxEmbeddedStderrBytes))
	if !strings.HasSuffix(got, stderrTruncatedSuffix) {
		t.Fatalf("missing truncation suffix: %.40q", got[len(got)-40:])
	}
	body := strings.TrimSuffix(got, stderrTruncatedSuffix)
	if !utf8.ValidString(body) || len(body) > maxEmbeddedStderrBytes {
		t.Errorf("body invalid or over cap: valid=%v len=%d", utf8.ValidString(body), len(body))
	}
	if short := embeddedStderr("boom"); short != "boom" {
		t.Errorf("short stderr changed: %q", short)
	}
}

func recordedArgv(t *testing.T, s *YtDlpAudioSearcher, call func(ctx context.Context, s *YtDlpAudioSearcher, outDir string)) []string {
	t.Helper()
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	var argvFile string
	s.binary, argvFile = argvRecordingDownloaderAt(t, outDir)
	pathDir := t.TempDir()
	if err := os.Symlink(s.binary, filepath.Join(pathDir, "yt-dlp")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	call(context.Background(), s, outDir)
	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

var ytDlpCallers = map[string]func(ctx context.Context, s *YtDlpAudioSearcher, outDir string){
	"search": func(ctx context.Context, s *YtDlpAudioSearcher, _ string) {
		_, _ = s.runYtDlpSearch(ctx, "ytsearch5:x")
	},
	"download": func(ctx context.Context, s *YtDlpAudioSearcher, outDir string) {
		_, _ = s.Download(ctx, "https://youtube.com/watch?v=1", outDir)
	},
	"inspect": func(ctx context.Context, s *YtDlpAudioSearcher, _ string) {
		_, _ = s.runYtDlpInspect(ctx, "https://soundcloud.com/a/b")
	},
	"canary": func(ctx context.Context, s *YtDlpAudioSearcher, _ string) {
		_ = s.Canary(ctx, YouTubeCanary)
	},
}

func TestYtDlpAudioSearcher_RemoteComponentFlags_PerCaller(t *testing.T) {
	cases := []struct {
		name string
		with func(s *YtDlpAudioSearcher) *YtDlpAudioSearcher
		want []string
	}{
		{
			"default fetches ejs:github", func(s *YtDlpAudioSearcher) *YtDlpAudioSearcher { return s },
			[]string{"--js-runtimes", "node", "--remote-components", "ejs:github"},
		},
		{
			"configured spec", func(s *YtDlpAudioSearcher) *YtDlpAudioSearcher { return s.WithRemoteComponents("ejs:npm") },
			[]string{"--js-runtimes", "node", "--remote-components", "ejs:npm"},
		},
		{
			"empty spec drops the fetch", func(s *YtDlpAudioSearcher) *YtDlpAudioSearcher { return s.WithRemoteComponents("") },
			[]string{"--js-runtimes", "node"},
		},
	}
	for caller, call := range ytDlpCallers {
		for _, tc := range cases {
			t.Run(caller+"/"+tc.name, func(t *testing.T) {
				s := tc.with(NewYtDlpAudioSearcher("", "", "node"))
				argv := recordedArgv(t, s, call)
				if len(argv) < len(tc.want) || strings.Join(argv[:len(tc.want)], " ") != strings.Join(tc.want, " ") {
					t.Fatalf("argv = %q, want it to start with %q", argv, tc.want)
				}
				if len(tc.want) == 2 && strings.Contains(strings.Join(argv, " "), "--remote-components") {
					t.Fatalf("argv = %q, want no --remote-components", argv)
				}
			})
		}
	}
}

func TestYtDlpAudioSearcher_NoJSRuntimeEmitsNoComponentFlags(t *testing.T) {
	for caller, call := range ytDlpCallers {
		t.Run(caller, func(t *testing.T) {
			argv := strings.Join(recordedArgv(t, NewYtDlpAudioSearcher("", "", ""), call), " ")
			if strings.Contains(argv, "--js-runtimes") || strings.Contains(argv, "--remote-components") {
				t.Fatalf("argv = %q, want no runtime or component flags", argv)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_ReportsTheFormatYtDlpFetched(t *testing.T) {
	cases := []struct {
		name, line string
		want       ports.SourceFormat
	}{
		{"audio-only opus", "251|opus|129.6", ports.SourceFormat{ID: "251", Codec: "opus", Kbps: 130}},
		{"combined video has no audio bitrate", "18|mp4a.40.2|NA", ports.SourceFormat{ID: "18", Codec: "mp4a.40.2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := downloadWithScript(t, "echo 'altune-source-format:"+tc.line+"'; truncate -s 20K OUT/a.m4a; echo altune-output-path:OUT/a.m4a")
			if err != nil {
				t.Fatal(err)
			}
			if got.Format != tc.want {
				t.Errorf("Format = %+v, want %+v", got.Format, tc.want)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_WithoutAFormatLineStillReturnsTheFile(t *testing.T) {
	got, err := downloadWithScript(t, "truncate -s 20K OUT/a.m4a; echo altune-output-path:OUT/a.m4a")
	if err != nil || filepath.Base(got.Path) != "a.m4a" || got.Format != (ports.SourceFormat{}) {
		t.Fatalf("Download = %+v, %v, want a.m4a with an unknown format", got, err)
	}
}

func TestYtDlpAudioSearcher_Download_AsksForTheFormatOnlyOnceTheFileIsFinal(t *testing.T) {
	withDecoyYtDlp(t)
	outDir := t.TempDir()
	binary, argvFile := argvRecordingDownloaderAt(t, outDir)
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary

	if _, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	print, separator := slices.Index(argv, "--print"), slices.Index(argv, "--")
	if print < 0 || print > separator || !strings.HasPrefix(argv[print+1], "after_move:altune-source-format:") {
		t.Fatalf("argv = %q, want --print after_move:altune-source-format:... before --", argv)
	}
}

func TestYtDlpAudioSearcher_POTokenURL_ReachesEveryCaller(t *testing.T) {
	const potURL = "http://altune-pot:4416"
	for caller, call := range ytDlpCallers {
		t.Run(caller, func(t *testing.T) {
			argv := recordedArgv(t, NewYtDlpAudioSearcher("", "", "node").WithPOTokenURL(potURL), call)
			if !hasFlagValue(argv, "--extractor-args", "youtubepot-bgutilhttp:base_url="+potURL) {
				t.Fatalf("argv = %q, want --extractor-args youtubepot-bgutilhttp:base_url=%s", argv, potURL)
			}
		})
	}
}

func TestYtDlpAudioSearcher_NoPOTokenURLEmitsNoExtractorArgs(t *testing.T) {
	for caller, call := range ytDlpCallers {
		t.Run(caller, func(t *testing.T) {
			argv := recordedArgv(t, NewYtDlpAudioSearcher("", "", "node"), call)
			if strings.Contains(strings.Join(argv, " "), "youtubepot") {
				t.Fatalf("argv = %q, want no token provider flag", argv)
			}
		})
	}
}

func hasFlagValue(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func TestYtDlpURL_SendsYouTubeWatchLinksThroughYouTubeMusic(t *testing.T) {
	cases := map[string]string{
		"https://www.youtube.com/watch?v=XDC3uH1FSgw":                "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		"https://youtube.com/watch?v=XDC3uH1FSgw":                    "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		"https://m.youtube.com/watch?v=XDC3uH1FSgw":                  "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		"https://youtu.be/XDC3uH1FSgw":                               "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		"https://www.youtube.com/watch?v=XDC3uH1FSgw&list=RDXDC&t=4": "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		"https://music.youtube.com/watch?v=XDC3uH1FSgw":              "https://music.youtube.com/watch?v=XDC3uH1FSgw",
		"https://soundcloud.com/prettifun/rollacoasta":               "https://soundcloud.com/prettifun/rollacoasta",
		"https://www.youtube.com/playlist?list=PL123":                "https://www.youtube.com/playlist?list=PL123",
		"https://www.youtube.com/watch?v=short":                      "https://www.youtube.com/watch?v=short",
		"https://notyoutube.com/watch?v=XDC3uH1FSgw":                 "https://notyoutube.com/watch?v=XDC3uH1FSgw",
	}
	for in, want := range cases {
		if got := ytDlpURL(in); got != want {
			t.Errorf("ytDlpURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestYtDlpAudioSearcher_DownloadPreviewAndCanaryFetchYouTubeThroughYouTubeMusic(t *testing.T) {
	for caller, call := range map[string]func(ctx context.Context, s *YtDlpAudioSearcher, outDir string){
		"download": func(ctx context.Context, s *YtDlpAudioSearcher, outDir string) {
			_, _ = s.Download(ctx, "https://www.youtube.com/watch?v=XDC3uH1FSgw", outDir)
		},
		"preview": func(ctx context.Context, s *YtDlpAudioSearcher, outDir string) {
			_, _ = s.DownloadPreview(ctx, "https://youtu.be/XDC3uH1FSgw", outDir, 30)
		},
		"canary": ytDlpCallers["canary"],
	} {
		t.Run(caller, func(t *testing.T) {
			argv := recordedArgv(t, NewYtDlpAudioSearcher("", "", ""), call)
			last := argv[len(argv)-1]
			if !strings.HasPrefix(last, "https://music.youtube.com/watch?v=") {
				t.Fatalf("yt-dlp got %q, want a music.youtube.com watch URL", last)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_AsksForAACIntoM4AWithAFixedEncodeBitrate(t *testing.T) {
	argv := strings.Join(recordedArgv(t, NewYtDlpAudioSearcher("", "", ""), ytDlpCallers["download"]), "\n")
	for _, want := range []string{
		"-f\n140/bestaudio[ext=m4a]/bestaudio/",
		"--audio-format\nm4a",
		"--audio-quality\n" + fmt.Sprintf("%dK", aacEncodeKbps),
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv = %q, want it to contain %q", argv, want)
		}
	}
	if strings.Contains(argv, "mp3") {
		t.Errorf("argv = %q, want no mp3 output", argv)
	}
}

func TestYtDlpAudioSearcher_Download_ReturnsThePrintedFileAndIgnoresLeftovers(t *testing.T) {
	got, err := downloadWithScript(t, "truncate -s 20K OUT/a.m4a; truncate -s 80K OUT/a.webm; truncate -s 90K OUT/a.m4a.part; truncate -s 70K OUT/a.mp4; echo altune-output-path:OUT/a.m4a")
	if err != nil || filepath.Base(got.Path) != "a.m4a" {
		t.Fatalf("Download = %q, %v, want a.m4a", got.Path, err)
	}
}

func TestYtDlpAudioSearcher_Download_RejectsWhenNoM4AIsProduced(t *testing.T) {
	_, err := downloadWithScript(t, "truncate -s 40K OUT/a.mp3; echo altune-output-path:OUT/a.mp3")
	if err == nil || !strings.Contains(err.Error(), "not an m4a") {
		t.Fatalf("Download error = %v, want a not-m4a rejection", err)
	}
}

func TestYtDlpEntry_ASearchResultIsAnUpload(t *testing.T) {
	c := CandidateFromEntry(Entry{Title: "Bruno Mars - Finesse", WebpageURL: "https://www.youtube.com/watch?v=x"})

	if c.Kind != ports.KindUpload {
		t.Errorf("Kind = %q, want an upload", c.Kind)
	}
}

func previewWithScript(t *testing.T, body string) (string, error) {
	t.Helper()
	outDir := t.TempDir()
	binary := filepath.Join(t.TempDir(), "fake-yt-dlp")
	script := "#!/bin/sh\n" + strings.ReplaceAll(body, "OUT", outDir) + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = binary
	return s.DownloadPreview(context.Background(), "https://youtube.com/watch?v=1", outDir, 30)
}

func TestDownload_RejectsAPrintedPathThatClimbsOutOfTheOutputDir(t *testing.T) {
	_, err := downloadWithScript(t, "truncate -s 20K OUT/../escaped.m4a; echo 'altune-output-path:OUT/../escaped.m4a'")
	if err == nil {
		t.Fatal("Download accepted a printed path that climbs out of the output dir with ../")
	}
}

func TestDownload_RejectsAPrintedPathInASiblingDirSharingTheOutputDirPrefix(t *testing.T) {
	_, err := downloadWithScript(t, "mkdir -p OUT-evil; truncate -s 20K OUT-evil/a.m4a; echo 'altune-output-path:OUT-evil/a.m4a'")
	if err == nil {
		t.Fatal("Download accepted a printed path in a sibling dir whose name starts with the output dir")
	}
}

func TestDownload_RejectsAPrintedPathThatDoesNotExist(t *testing.T) {
	_, err := downloadWithScript(t, "echo 'altune-output-path:OUT/missing.m4a'")
	if err == nil {
		t.Fatal("Download accepted a printed path with no file behind it")
	}
}

func TestDownload_AnEmptyPrintedPathIsNoPath(t *testing.T) {
	_, err := downloadWithScript(t, "truncate -s 20K OUT/a.m4a; echo 'altune-output-path:'")
	if err == nil || err.Error() != "no output path printed by yt-dlp" {
		t.Fatalf("Download error = %v, want no output path printed by yt-dlp", err)
	}
}

func TestDownload_APathPrintedOnlyToStderrIsNoPath(t *testing.T) {
	_, err := downloadWithScript(t, "truncate -s 20K OUT/a.m4a; echo 'altune-output-path:OUT/a.m4a' >&2")
	if err == nil || err.Error() != "no output path printed by yt-dlp" {
		t.Fatalf("Download error = %v, want no output path printed by yt-dlp", err)
	}
}

func TestDownload_ReturnsAPrintedPathWithUnicodeSpacesAndAmpersand(t *testing.T) {
	got, err := downloadWithScript(t, "truncate -s 20K 'OUT/Sigur Rós - Hoppípolla (Simon & Garfunkel).m4a'; truncate -s 90K OUT/stray.m4a; echo 'altune-source-format:hls_aac_160k|mp4a.40.2|160'; echo 'altune-output-path:OUT/Sigur Rós - Hoppípolla (Simon & Garfunkel).m4a'")
	if err != nil || filepath.Base(got.Path) != "Sigur Rós - Hoppípolla (Simon & Garfunkel).m4a" {
		t.Fatalf("Download = %q, %v, want the printed unicode file", got.Path, err)
	}
}

func TestDownload_FindsThePrintedPathAfterAHugeStdout(t *testing.T) {
	got, err := downloadWithScript(t, "truncate -s 20K OUT/a.m4a; truncate -s 90K OUT/stray.m4a; head -c 2000000 /dev/zero | tr '\\000' x; echo; seq 1 100000; echo 'altune-output-path:OUT/a.m4a'")
	if err != nil || filepath.Base(got.Path) != "a.m4a" {
		t.Fatalf("Download = %q, %v, want a.m4a after a huge stdout", got.Path, err)
	}
}

func TestDownloadPreview_ReturnsThePrintedFileNotALargerStray(t *testing.T) {
	got, err := previewWithScript(t, "truncate -s 20K OUT/preview.m4a; truncate -s 90K OUT/stray.m4a; echo 'altune-output-path:OUT/preview.m4a'")
	if err != nil || filepath.Base(got) != "preview.m4a" {
		t.Fatalf("DownloadPreview = %q, %v, want preview.m4a", got, err)
	}
}

func TestDownloadPreview_ErrorsWhenNoPathIsPrinted(t *testing.T) {
	_, err := previewWithScript(t, "truncate -s 20K OUT/preview.m4a")
	if err == nil || !strings.Contains(err.Error(), "no output path printed by yt-dlp") {
		t.Fatalf("DownloadPreview error = %v, want no output path printed by yt-dlp", err)
	}
}
