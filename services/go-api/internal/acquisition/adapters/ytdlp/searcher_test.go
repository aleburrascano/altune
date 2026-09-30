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
	"slices"
	"strings"
	"testing"
	"time"
)

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
			if got := ports.IsSourceUnavailable(err); got != tt.wantUnavailable {
				t.Errorf("IsSourceUnavailable(%v) = %v, want %v", err, got, tt.wantUnavailable)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_MissingBinaryIsAnUnavailableSource(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp-absent")

	_, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", t.TempDir())

	if !ports.IsSourceUnavailable(err) {
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

func formatArgRecorder(t *testing.T, outDir, stdout string) (binary, argsFile string) {
	t.Helper()
	argsFile = filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > \"" + argsFile + "\"\n"
	if outDir != "" {
		script += "truncate -s 1M \"" + filepath.Join(outDir, "out.mp3") + "\"\n"
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
	alternatives := strings.Split(got, "/")
	if alternatives[0] != "bestaudio" {
		t.Errorf("-f %q, want audio-only streams preferred first", got)
	}
	if len(alternatives) < 2 || !strings.HasPrefix(alternatives[1], "best") || alternatives[1] == "bestaudio" {
		t.Errorf("-f %q, want a combined-format fallback after bestaudio", got)
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
		"head -c 20480 /dev/zero > \"" + filepath.Join(outDir, "stub.mp3") + "\"\n"
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
		return nil, &ports.SourceUnavailableError{Source: SourceName, Err: errors.New("throttled")}
	})

	_, err := s.SearchQueries(context.Background(), []string{"a", "b"})

	if !ports.IsSourceUnavailable(err) {
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
