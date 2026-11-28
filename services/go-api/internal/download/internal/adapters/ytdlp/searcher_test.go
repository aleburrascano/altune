package ytdlp_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	secretCookiePath    = "/secret/cookies.txt"
	soundCloudTrackURL  = "https://soundcloud.com/bran-van-3000/drinking-in-l-a-3"
	stderrTruncated     = "...(truncated)"
	embeddedStderrLimit = 2048
)

func TestYtDlpAudioSearcher_Search(t *testing.T) {
	if os.Getenv("YTDLP_LIVE") != "1" {
		t.Skip("set YTDLP_LIVE=1 to search YouTube and SoundCloud with the real yt-dlp")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		t.Fatalf("YTDLP_LIVE=1 but yt-dlp is not on PATH: %v", err)
	}

	searcher := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep)
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

func downloadArgv(t *testing.T, s *ytdlp.YouTubeAudioSearcher, url string) []string {
	t.Helper()
	outDir := t.TempDir()
	calls := installYtDlp(t, stubDownload(outDir))
	if _, err := s.Download(context.Background(), url, outDir); err != nil {
		t.Fatalf("Download error: %v", err)
	}
	return onlyCall(t, calls)
}

func TestDownload_MaxFilesizeFlagIsTheDownloadedFileCap(t *testing.T) {
	argv := downloadArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep), youTubeWatchURL)

	if got := flagValue(t, argv, "--max-filesize"); got != "209715200" {
		t.Fatalf("--max-filesize %q, want the 200 MiB downloaded-file cap", got)
	}
}

func searchOneQuery(t *testing.T) ([]domain.AudioCandidate, error) {
	t.Helper()
	return ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).SearchQueries(context.Background(), []string{"q"})
}

func TestSearch_OutputThatParsesToNothingIsAnError(t *testing.T) {
	installSearch(t, "printf 'garbage\\nmore garbage\\n'")

	_, err := searchOneQuery(t)

	if err == nil {
		t.Fatal("unparsable output reported as a successful empty search, want an error")
	}
	if !strings.Contains(err.Error(), "2 lines, 0 parsable") {
		t.Fatalf("error = %v, want it to carry the line and parsable counts", err)
	}
}

func TestSearch_MixedOutputKeepsGoodEntriesAndLogsSkipped(t *testing.T) {
	installSearch(t, `printf '%s\ngarbage\n%s\n' '{"title":"Good","webpage_url":"https://youtube.com/watch?v=aaaaaaaaaaa","duration":12}' '{"title":"No URL"}'`)
	logs := logging.CaptureJSON(t, slog.LevelDebug)

	got, err := searchOneQuery(t)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].URL != "https://youtube.com/watch?v=aaaaaaaaaaa" {
		t.Fatalf("candidates = %+v, want only the parsable entry with a URL", got)
	}
	logged := logs.String()
	if !strings.Contains(logged, "acquisition.search_lines_skipped") || !strings.Contains(logged, `"skipped":2`) {
		t.Fatalf("expected a warn line counting the 2 skipped lines, got:\n%s", logged)
	}
}

func TestSearch_NoOutputIsAnEmptyResult(t *testing.T) {
	installSearch(t, "true")

	got, err := searchOneQuery(t)
	if err != nil {
		t.Fatalf("a search that found nothing must not error, got: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("candidates = %+v, want none", got)
	}
}

func TestSearch_ClassifiesASourceThatRefusedToAnswer(t *testing.T) {
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
			installSearch(t, "fail '"+tt.stderr+"'")

			_, err := searchOneQuery(t)

			if err == nil {
				t.Fatal("a yt-dlp that exited 1 reported no error")
			}
			if got := isSourceUnavailable(err); got != tt.wantUnavailable {
				t.Errorf("source unavailable(%v) = %v, want %v", err, got, tt.wantUnavailable)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_MissingBinaryIsAnUnavailableSource(t *testing.T) {
	withoutAnyBinaries(t)

	_, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Download(context.Background(), youTubeWatchURL, t.TempDir())

	if !isSourceUnavailable(err) {
		t.Errorf("Download error = %v, want a missing yt-dlp to read as an unavailable source", err)
	}
}

func TestYtDlpAudioSearcher_Search_EngineFailureLogRedactsTheCookiePath(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	s := ytdlp.MustNewYtDlpAudioSearcher("", secretCookiePath, "", time.Now, clock.Sleep)

	if _, err := s.SearchQueries(context.Background(), []string{"q"}); err == nil {
		t.Fatal("every engine failed, Search reported no error")
	}

	logged := loggedErrors(t, logs, "acquisition.engine_search_failed")
	if len(logged) != 2 {
		t.Fatalf("engine failure logs = %q, want one per engine", logged)
	}
	for _, line := range logged {
		if strings.Contains(line, "/secret") {
			t.Fatalf("the cookie jar path leaked into the log: %s", line)
		}
		if !strings.Contains(line, "no such file or directory") {
			t.Fatalf("redaction dropped the diagnostic text: %s", line)
		}
	}
}

func TestNewYtDlpAudioSearcher_ReturnsNonNil(t *testing.T) {
	if ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep) == nil {
		t.Fatal("NewYtDlpAudioSearcher returned nil")
	}
}

func TestNewYtDlpAudioSearcher_PassesItsConfigToYtDlp(t *testing.T) {
	jar := cookieJar(t, "cookies.txt", "jar")
	argv := downloadArgv(t, ytdlp.MustNewYtDlpAudioSearcher("/usr/bin/ffmpeg", jar, "deno", time.Now, clock.Sleep), youTubeWatchURL)

	if !hasFlagValue(argv, "--ffmpeg-location", "/usr/bin/ffmpeg") {
		t.Errorf("argv = %q, want --ffmpeg-location /usr/bin/ffmpeg", argv)
	}
	if !slices.Contains(argv, "--cookies") {
		t.Errorf("argv = %q, want the configured cookie jar passed with --cookies", argv)
	}
	if !hasFlagValue(argv, "--js-runtimes", "deno") {
		t.Errorf("argv = %q, want --js-runtimes deno", argv)
	}
}

func TestYtDlpAudioSearcher_Available_MissingBinary(t *testing.T) {
	withoutAnyBinaries(t)
	if ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Available() {
		t.Error("Available() = true for a missing yt-dlp binary, want false")
	}
}

func TestYtDlpAudioSearcher_Available_PresentBinary(t *testing.T) {
	withoutAnyBinaries(t)
	installYtDlp(t, "")
	if !ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Available() {
		t.Error("Available() = false for a present yt-dlp binary, want true")
	}
}

func TestYtDlpAudioSearcher_Search_QueriesBothEngines(t *testing.T) {
	calls := installSearch(t, "emit 1")

	got, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).SearchQueries(context.Background(), []string{"song artist"})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	if specs := searchedSpecs(t, calls); !slices.Equal(specs, []string{"scsearch5:song artist", "ytsearch5:song artist"}) {
		t.Fatalf("engine specs = %v, want yt and sc", specs)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 merged candidates, got %d: %+v", len(got), got)
	}
}

func TestYtDlpAudioSearcher_Search_DedupsByURL(t *testing.T) {
	installSearch(t, `echo '{"title":"Dup","webpage_url":"https://soundcloud.com/x/same"}'`)

	got, err := searchOneQuery(t)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected duplicate URL collapsed to 1, got %d", len(got))
	}
}

func TestYtDlpAudioSearcher_Search_OneEngineFails(t *testing.T) {
	installSearch(t, "emit 1", searchAnswer{"ytsearch5:q", "fail 'youtube blew up'"})

	got, err := searchOneQuery(t)
	if err != nil {
		t.Fatalf("a single engine failure must not fail the search, got: %v", err)
	}
	if len(got) != 1 || got[0].Title != "scsearch5:q" {
		t.Fatalf("expected the surviving engine's candidate, got %+v", got)
	}
}

func TestYtDlpAudioSearcher_Search_BothEnginesFail(t *testing.T) {
	installSearch(t, "fail 'down'")

	if _, err := searchOneQuery(t); err == nil {
		t.Fatal("expected an error when every engine fails")
	}
}

func TestYtDlpAudioSearcher_Download_RejectsAnOversizeOutput(t *testing.T) {
	_, err := downloadWithScript(t, "truncate -s 210M OUT/big.m4a; echo altune-output-path:OUT/big.m4a")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("Download error = %v, want a too-large rejection", err)
	}
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

func TestDownload_ReadsRealYtDlpStdout(t *testing.T) {
	stdout, err := filepath.Abs(filepath.Join("testdata", "ytdlp_2026.08.19_soundcloud_download_stdout.txt"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := downloadWithScript(t, "truncate -s 20K 'OUT/The Sound Of Silence (Simon & Garfunkel).m4a'; sed 's|/tmp/dl2 out|OUT|' '"+stdout+"'")
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}

	if want := "The Sound Of Silence (Simon & Garfunkel).m4a"; filepath.Base(got.Path) != want {
		t.Fatalf("Download path = %q, want %q", got.Path, want)
	}
	if want := (domain.SourceFormat{ID: "hls_aac_160k", Codec: "mp4a.40.2", Kbps: 160}); got.Format != want {
		t.Fatalf("Download format = %+v, want %+v", got.Format, want)
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

func TestDownload_TimeoutUnderLiveParentIsSourceUnavailable(t *testing.T) {
	installYtDlp(t, "exec sleep 30")
	s := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep, ytdlp.WithDownloadTimeout(100*time.Millisecond))

	_, err := s.Download(context.Background(), youTubeWatchURL, t.TempDir())

	if !isSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a source-unavailable error", err)
	}
}

func TestDownload_ParentCancellationIsNotSourceUnavailable(t *testing.T) {
	installYtDlp(t, "exec sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Download(ctx, youTubeWatchURL, t.TempDir())

	if err == nil || isSourceUnavailable(err) {
		t.Fatalf("Download err = %v, want a plain error", err)
	}
}

func TestDownload_FallsBackToACombinedFormatWhenNoAudioOnlyStreamExists(t *testing.T) {
	got := downloadFormat(t)

	audioOnly, combined, ok := strings.Cut(got, "/best[")
	if !ok || !strings.HasSuffix(audioOnly, "/bestaudio") {
		t.Errorf("-f %q, want audio-only streams preferred before any combined format", got)
	}
	if !strings.HasSuffix(combined, "/best") {
		t.Errorf("-f %q, want a combined-format fallback after the audio-only streams", got)
	}
}

func jarWritingYtDlp(t *testing.T, outDir string) {
	t.Helper()
	installYtDlp(t, "prev=\"\"\n"+
		"for arg in \"$@\"; do\n"+
		"  if [ \"$prev\" = \"--cookies\" ]; then echo '# saved by yt-dlp' > \"$arg\" || exit 1; fi\n"+
		"  prev=\"$arg\"\n"+
		"done\n"+
		"echo '{\"title\":\"Song\",\"webpage_url\":\"https://www.youtube.com/watch?v=aaaaaaaaaaa\"}'\n"+
		stubDownload(outDir))
}

func readOnlyLiveJar(t *testing.T) string {
	t.Helper()
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte(liveJarContents), 0o444); err != nil {
		t.Fatal(err)
	}
	return jar
}

func assertLiveJarRenewedAndStillReadOnly(t *testing.T, jar string) {
	t.Helper()
	got, err := os.ReadFile(jar)
	if err != nil || string(got) != "# saved by yt-dlp\n" {
		t.Fatalf("live cookie jar = %q, err %v, want the cookies yt-dlp saved", got, err)
	}
	if info, err := os.Stat(jar); err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("live cookie jar stat = %v, %v, want it still read-only", info, err)
	}
}

func TestYtDlpAudioSearcher_Search_WorksAgainstAReadOnlyCookieJarAndRenewsIt(t *testing.T) {
	jarWritingYtDlp(t, t.TempDir())
	jar := readOnlyLiveJar(t)

	got, err := ytdlp.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep).SearchQueries(context.Background(), []string{"song"})
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("search returned %d candidates, want 1", len(got))
	}
	assertLiveJarRenewedAndStillReadOnly(t, jar)
}

func TestYtDlpAudioSearcher_Download_WorksAgainstAReadOnlyCookieJarAndRenewsIt(t *testing.T) {
	outDir := t.TempDir()
	jarWritingYtDlp(t, outDir)
	jar := readOnlyLiveJar(t)

	s := ytdlp.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep)
	if _, err := s.Download(context.Background(), "https://www.youtube.com/watch?v=aaaaaaaaaaa", outDir); err != nil {
		t.Fatalf("download error: %v", err)
	}
	assertLiveJarRenewedAndStillReadOnly(t, jar)
}

func TestYtDlpAudioSearcher_SearchQueries_MergesEveryQueryAndEngine(t *testing.T) {
	installSearch(t, "emit 1")

	got, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).SearchQueries(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("SearchQueries error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("merged candidates = %d, want 4 (every query against both engines)", len(got))
	}
}

func TestYtDlpAudioSearcher_SearchQueries_AllQueriesUnavailableIsSourceUnavailable(t *testing.T) {
	installSearch(t, "fail 'ERROR: HTTP Error 429: Too Many Requests'")

	_, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).SearchQueries(context.Background(), []string{"a", "b"})

	if !isSourceUnavailable(err) {
		t.Fatalf("SearchQueries error = %v, want a source-unavailable error when every query and engine is unavailable", err)
	}
}

func TestYtDlpAudioSearcher_DownloadPreview_RequestsOnlyTheLeadingSectionBeforeTheURL(t *testing.T) {
	outDir := t.TempDir()
	calls := installYtDlp(t, stubDownload(outDir))

	if _, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).DownloadPreview(context.Background(), youTubeWatchURL, outDir, 130); err != nil {
		t.Fatalf("DownloadPreview error: %v", err)
	}

	argv := onlyCall(t, calls)
	sections, separator := slices.Index(argv, "--download-sections"), slices.Index(argv, "--")
	if sections < 0 || argv[sections+1] != "*0-130" || sections > separator {
		t.Fatalf("argv = %q, want --download-sections *0-130 before --", argv)
	}
}

func inspectedAs(json string) string {
	return ytDlpByMode("", "echo '"+json+"'", "")
}

func TestYtDlpAudioSearcher_MarkUnplayable_SetsTheExactPersistedReasonStrings(t *testing.T) {
	cases := []struct {
		name       string
		inspection string
		want       string
	}{
		{"drm", `{"duration":240,"formats":[{"format_id":"hls_aac_160k_encrypted","vcodec":"none"}]}`, "drm"},
		{"preview", `{"duration":30}`, "preview"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installYtDlp(t, inspectedAs(tc.inspection))
			candidates := []domain.AudioCandidate{downloadtest.Candidate(
				soundCloudTrackURL,
				downloadtest.WithTitle("Drinking in L.A."),
				downloadtest.WithDuration(240),
			)}

			ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).MarkUnplayable(context.Background(), candidates)

			if got := candidates[0].Unplayable; got != tc.want {
				t.Fatalf("Unplayable = %q, want %q", got, tc.want)
			}
		})
	}
}

type panickingContext struct{}

func (panickingContext) Deadline() (time.Time, bool) { panic("deadline exploded") }
func (panickingContext) Done() <-chan struct{}       { return nil }
func (panickingContext) Err() error                  { return nil }
func (panickingContext) Value(any) any               { return nil }

func TestYtDlpAudioSearcher_SearchQueries_APanicInsideEverySearchIsAFailureNotAnEmptyAnswer(t *testing.T) {
	candidates, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).
		SearchQueries(panickingContext{}, []string{"Blinding Lights"})

	if err == nil {
		t.Fatalf("SearchQueries = %d candidates, nil; want an error so the track is not reported as having no match", len(candidates))
	}
}

func TestYtDlpAudioSearcher_MarkUnplayable_APanicInsideAnInspectionLeavesTheCandidatePlayable(t *testing.T) {
	candidates := []domain.AudioCandidate{downloadtest.Candidate(
		soundCloudTrackURL,
		downloadtest.WithTitle("Drinking in L.A."),
		downloadtest.WithDuration(240),
	)}

	ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).MarkUnplayable(panickingContext{}, candidates)

	if got := candidates[0].Unplayable; got != "" {
		t.Errorf("Unplayable = %q, want it left empty", got)
	}
}

const leakyStderr = "echo \"ERROR: unable to open /home/op/secrets/cookies.txt https://cdn.example/a?Expires=1&token=SIGNEDTOKEN123456&Signature=SIGNEDSIG987654\" >&2\n" +
	"head -c 100000 /dev/zero | tr '\\0' 'x' >&2\n" +
	"exit 1"

func assertStderrEmbeddedSafely(t *testing.T, msg string) {
	t.Helper()
	if !strings.Contains(msg, "(stderr: ") {
		t.Fatalf("error lost its stderr section (binary not run?): %q", msg)
	}
	for _, leak := range []string{"cookies.txt", "SIGNEDTOKEN123456", "SIGNEDSIG987654"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error leaks %q: %.300s", leak, msg)
		}
	}
	if limit := embeddedStderrLimit + 512; len(msg) > limit {
		t.Errorf("error is %d bytes, want <= %d", len(msg), limit)
	}
}

func failedError(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error from the failing binary")
	}
	return err.Error()
}

func TestYtDlpAudioSearcher_SearchRedactsAndCapsStderr(t *testing.T) {
	installYtDlp(t, leakyStderr)
	_, err := searchOneQuery(t)
	assertStderrEmbeddedSafely(t, failedError(t, err))
}

func TestYtDlpAudioSearcher_DownloadRedactsAndCapsStderr(t *testing.T) {
	installYtDlp(t, leakyStderr)
	_, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Download(context.Background(), "https://example.com/v", t.TempDir())
	assertStderrEmbeddedSafely(t, failedError(t, err))
}

func TestYtDlpAudioSearcher_InspectFailureLogCapsStderr(t *testing.T) {
	installYtDlp(t, leakyStderr)
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	candidates := []domain.AudioCandidate{downloadtest.Candidate(soundCloudTrackURL, downloadtest.WithDuration(240))}

	ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).MarkUnplayable(context.Background(), candidates)

	logged := loggedErrors(t, logs, "acquisition.soundcloud_inspect_failed")
	if len(logged) != 1 {
		t.Fatalf("inspect failure logs = %d, want 1", len(logged))
	}
	assertStderrEmbeddedSafely(t, logged[0])
}

func embeddedDownloadStderr(t *testing.T, stderr string) string {
	t.Helper()
	installYtDlp(t, "printf '%s' '"+stderr+"' >&2\nexit 1")
	_, err := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Download(context.Background(), youTubeWatchURL, t.TempDir())
	_, embedded, found := strings.Cut(failedError(t, err), "(stderr: ")
	if !found {
		t.Fatalf("Download error = %v, want an embedded stderr section", err)
	}
	return strings.TrimSuffix(embedded, ")")
}

func TestDownload_EmbeddedStderrIsCappedOnARuneBoundaryWithASuffix(t *testing.T) {
	got := embeddedDownloadStderr(t, "x"+strings.Repeat("é", embeddedStderrLimit))
	body, truncated := strings.CutSuffix(got, stderrTruncated)
	if !truncated {
		t.Fatalf("missing truncation suffix: %.40q", got[len(got)-40:])
	}
	if !utf8.ValidString(body) || len(body) > embeddedStderrLimit {
		t.Errorf("body invalid or over cap: valid=%v len=%d", utf8.ValidString(body), len(body))
	}
	if short := embeddedDownloadStderr(t, "boom"); short != "boom" {
		t.Errorf("short stderr changed: %q", short)
	}
}

type ytDlpCaller struct {
	marker string
	call   func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, outDir string)
}

var ytDlpCallers = map[string]ytDlpCaller{
	"search": {"--dump-json", func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, _ string) {
		_, _ = s.SearchQueries(ctx, []string{"x"})
	}},
	"download": {"-x", func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, outDir string) {
		_, _ = s.Download(ctx, youTubeWatchURL, outDir)
	}},
	"inspect": {"-J", func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, _ string) {
		s.MarkUnplayable(ctx, []domain.AudioCandidate{downloadtest.Candidate("https://soundcloud.com/a/b")})
	}},
	"canary": {"--simulate", func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, _ string) {
		_ = s.Canary(ctx, ytdlp.YouTubeCanary)
	}},
}

func recordedArgv(t *testing.T, s *ytdlp.YouTubeAudioSearcher, caller ytDlpCaller) []string {
	t.Helper()
	outDir := t.TempDir()
	calls := installYtDlp(t, ytDlpByMode("", "echo '{\"duration\":240}'", stubDownload(outDir)))
	caller.call(context.Background(), s, outDir)
	return callWith(t, calls, caller.marker)
}

func TestYtDlpAudioSearcher_RemoteComponentFlags_PerCaller(t *testing.T) {
	cases := []struct {
		name string
		with []func(*ytdlp.YouTubeAudioSearcher)
		want []string
	}{
		{
			"default fetches ejs:github", nil,
			[]string{"--js-runtimes", "node", "--remote-components", "ejs:github"},
		},
		{
			name: "configured spec",
			with: []func(*ytdlp.YouTubeAudioSearcher){ytdlp.WithRemoteComponents("ejs:npm")},
			want: []string{"--js-runtimes", "node", "--remote-components", "ejs:npm"},
		},
		{
			name: "empty spec drops the fetch",
			with: []func(*ytdlp.YouTubeAudioSearcher){ytdlp.WithRemoteComponents("")},
			want: []string{"--js-runtimes", "node"},
		},
	}
	for name, caller := range ytDlpCallers {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				argv := recordedArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "node", time.Now, clock.Sleep, tc.with...), caller)
				if len(argv) < len(tc.want) || strings.Join(argv[:len(tc.want)], " ") != strings.Join(tc.want, " ") {
					t.Fatalf("argv = %q, want it to start with %q", argv, tc.want)
				}
				if len(tc.want) == 2 && slices.Contains(argv, "--remote-components") {
					t.Fatalf("argv = %q, want no --remote-components", argv)
				}
			})
		}
	}
}

func TestYtDlpAudioSearcher_NoJSRuntimeEmitsNoComponentFlags(t *testing.T) {
	for name, caller := range ytDlpCallers {
		t.Run(name, func(t *testing.T) {
			argv := recordedArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep), caller)
			if slices.Contains(argv, "--js-runtimes") || slices.Contains(argv, "--remote-components") {
				t.Fatalf("argv = %q, want no runtime or component flags", argv)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_ReportsTheFormatYtDlpFetched(t *testing.T) {
	cases := []struct {
		name, line string
		want       domain.SourceFormat
	}{
		{"audio-only opus", "251|opus|129.6", domain.SourceFormat{ID: "251", Codec: "opus", Kbps: 130}},
		{"combined video has no audio bitrate", "18|mp4a.40.2|NA", domain.SourceFormat{ID: "18", Codec: "mp4a.40.2"}},
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
	if err != nil || filepath.Base(got.Path) != "a.m4a" || got.Format != (domain.SourceFormat{}) {
		t.Fatalf("Download = %+v, %v, want a.m4a with an unknown format", got, err)
	}
}

func TestYtDlpAudioSearcher_Download_AsksForTheFormatOnlyOnceTheFileIsFinal(t *testing.T) {
	argv := downloadArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep), youTubeWatchURL)

	print, separator := slices.Index(argv, "--print"), slices.Index(argv, "--")
	if print < 0 || print > separator || !strings.HasPrefix(argv[print+1], "after_move:altune-source-format:") {
		t.Fatalf("argv = %q, want --print after_move:altune-source-format:... before --", argv)
	}
}

func TestYtDlpAudioSearcher_POTokenURL_ReachesEveryCaller(t *testing.T) {
	const potURL = "http://altune-pot:4416"
	for name, caller := range ytDlpCallers {
		t.Run(name, func(t *testing.T) {
			argv := recordedArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "node", time.Now, clock.Sleep, ytdlp.WithPOTokenURL(potURL)), caller)
			if !hasFlagValue(argv, "--extractor-args", "youtubepot-bgutilhttp:base_url="+potURL) {
				t.Fatalf("argv = %q, want --extractor-args youtubepot-bgutilhttp:base_url=%s", argv, potURL)
			}
		})
	}
}

func TestYtDlpAudioSearcher_NoPOTokenURLEmitsNoExtractorArgs(t *testing.T) {
	for name, caller := range ytDlpCallers {
		t.Run(name, func(t *testing.T) {
			argv := recordedArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "node", time.Now, clock.Sleep), caller)
			if strings.Contains(strings.Join(argv, " "), "youtubepot") {
				t.Fatalf("argv = %q, want no token provider flag", argv)
			}
		})
	}
}

func TestDownload_SendsYouTubeWatchLinksThroughYouTubeMusic(t *testing.T) {
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
		t.Run(in, func(t *testing.T) {
			argv := downloadArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep), in)
			if got := argv[len(argv)-1]; got != want {
				t.Errorf("Download(%q) fetched %q, want %q", in, got, want)
			}
		})
	}
}

func TestYtDlpAudioSearcher_DownloadPreviewAndCanaryFetchYouTubeThroughYouTubeMusic(t *testing.T) {
	for name, caller := range map[string]ytDlpCaller{
		"download": {"-x", func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, outDir string) {
			_, _ = s.Download(ctx, "https://www.youtube.com/watch?v=XDC3uH1FSgw", outDir)
		}},
		"preview": {"-x", func(ctx context.Context, s *ytdlp.YouTubeAudioSearcher, outDir string) {
			_, _ = s.DownloadPreview(ctx, "https://youtu.be/XDC3uH1FSgw", outDir, 30)
		}},
		"canary": ytDlpCallers["canary"],
	} {
		t.Run(name, func(t *testing.T) {
			argv := recordedArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep), caller)
			last := argv[len(argv)-1]
			if !strings.HasPrefix(last, "https://music.youtube.com/watch?v=") {
				t.Fatalf("yt-dlp got %q, want a music.youtube.com watch URL", last)
			}
		})
	}
}

func TestYtDlpAudioSearcher_Download_AsksForAACIntoM4AWithAFixedEncodeBitrate(t *testing.T) {
	argv := strings.Join(downloadArgv(t, ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep), youTubeWatchURL), "\n")
	for _, want := range []string{
		"-f\n140/bestaudio[ext=m4a]/bestaudio/",
		"--audio-format\nm4a",
		"--audio-quality\n128K",
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

func TestSearch_ASearchResultIsAnUpload(t *testing.T) {
	installSearch(t, `echo '{"title":"Bruno Mars - Finesse","webpage_url":"https://www.youtube.com/watch?v=aaaaaaaaaaa","duration":185,"channel":"Bruno Mars","categories":["Music"],"view_count":42}'`)

	got, err := searchOneQuery(t)
	if err != nil || len(got) != 1 {
		t.Fatalf("Search = %+v, %v, want the one entry", got, err)
	}
	want := downloadtest.Candidate(
		"https://www.youtube.com/watch?v=aaaaaaaaaaa",
		downloadtest.WithTitle("Bruno Mars - Finesse"),
		downloadtest.WithDuration(185),
		downloadtest.WithChannel("Bruno Mars"),
		downloadtest.WithCategories([]string{"Music"}),
		downloadtest.WithViewCount(42),
		downloadtest.WithKind(domain.KindUpload),
	)
	if c := got[0]; c.Title != want.Title || c.Duration != want.Duration || c.URL != want.URL || c.Channel != want.Channel ||
		!slices.Equal(c.Categories, want.Categories) || c.ViewCount != want.ViewCount || c.Kind != want.Kind {
		t.Errorf("candidate = %+v, want %+v", c, want)
	}
}

func previewWithScript(t *testing.T, body string) (string, error) {
	t.Helper()
	outDir := t.TempDir()
	installYtDlp(t, strings.ReplaceAll(body, "OUT", outDir))
	return ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).DownloadPreview(context.Background(), youTubeWatchURL, outDir, 30)
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

func TestDownload_ANonPositiveTimeoutKeepsTheDefault(t *testing.T) {
	outDir := t.TempDir()
	installYtDlp(t, stubDownload(outDir))
	s := ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep, ytdlp.WithDownloadTimeout(0))

	if _, err := s.Download(context.Background(), youTubeWatchURL, outDir); err != nil {
		t.Fatalf("Download error = %v, want nil: a zero timeout must leave the default in place", err)
	}
}

func TestDownload_UsesATempCopyOfTheCookieJarAndCleansItUp(t *testing.T) {
	outDir := t.TempDir()
	jar := cookieJar(t, "cookies.txt", liveJarContents)
	capture := captureCookieJar(t, stubDownload(outDir))

	if _, err := ytdlp.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep).Download(context.Background(), youTubeWatchURL, outDir); err != nil {
		t.Fatalf("Download error = %v, want nil", err)
	}

	capture.assertTempCopyUsedAndRemoved(t, jar)
}

func TestMarkUnplayable_InspectionUsesATempCopyOfTheCookieJarAndCleansItUp(t *testing.T) {
	jar := cookieJar(t, "cookies.txt", liveJarContents)
	capture := captureCookieJar(t, "echo '{\"duration\":240}'")

	ytdlp.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep).MarkUnplayable(context.Background(),
		[]domain.AudioCandidate{downloadtest.Candidate(soundCloudTrackURL, downloadtest.WithDuration(240))})

	capture.assertTempCopyUsedAndRemoved(t, jar)
}

func TestSearch_UsesATempCopyOfTheCookieJarAndCleansItUp(t *testing.T) {
	jar := cookieJar(t, "cookies.txt", liveJarContents)
	capture := captureCookieJar(t, "")

	if _, err := ytdlp.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep).SearchQueries(context.Background(), []string{"q"}); err != nil {
		t.Fatalf("SearchQueries error = %v, want nil", err)
	}

	capture.assertTempCopyUsedAndRemoved(t, jar)
}
