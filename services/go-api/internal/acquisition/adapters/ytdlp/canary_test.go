package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingCanaryBinary writes an executable that records its argv, copies
// whatever file follows --cookies to cookieContentFile (so a test can inspect
// what the probe actually sent before Canary's own cleanup removes it), and
// prints duration to stdout.
func recordingCanaryBinary(t *testing.T, argvFile, cookiePathFile, cookieContentFile, duration string) string {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "recording-yt-dlp")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argvFile + "\n" +
		"prev=\"\"\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--cookies\" ]; then\n" +
		"    printf '%s' \"$arg\" > " + cookiePathFile + "\n" +
		"    cp \"$arg\" " + cookieContentFile + "\n" +
		"  fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"echo '" + duration + "'\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestCanary_UsesATempCopyOfTheCookieJarAndCleansItUp(t *testing.T) {
	dir := t.TempDir()
	realCookieFile := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(realCookieFile, []byte("# real cookie jar contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	argvFile := filepath.Join(dir, "argv")
	cookiePathFile := filepath.Join(dir, "cookie-path")
	cookieContentFile := filepath.Join(dir, "cookie-content")

	s := NewYtDlpAudioSearcher("", realCookieFile, "")
	s.binary = recordingCanaryBinary(t, argvFile, cookiePathFile, cookieContentFile, "212")

	if err := s.Canary(context.Background(), YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	usedPath, err := os.ReadFile(cookiePathFile)
	if err != nil {
		t.Fatalf("probe never received a --cookies flag: %v", err)
	}
	if string(usedPath) == realCookieFile {
		t.Fatalf("probe used the live cookie file %q, want a temp copy", realCookieFile)
	}

	content, err := os.ReadFile(cookieContentFile)
	if err != nil || string(content) != "# real cookie jar contents" {
		t.Fatalf("temp cookie copy content = %q, err %v, want the real jar's contents", content, err)
	}

	if _, err := os.Stat(string(usedPath)); !os.IsNotExist(err) {
		t.Fatalf("temp cookie copy %q still exists after Canary returned, want it removed", usedPath)
	}
}

func TestCanary_CarriesTheSameAuthFlagsAsSearchAndDownload(t *testing.T) {
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookieFile, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	argvFile := filepath.Join(dir, "argv")

	s := NewYtDlpAudioSearcher("", cookieFile, "deno")
	s.binary = recordingCanaryBinary(t, argvFile, filepath.Join(dir, "cookie-path"), filepath.Join(dir, "cookie-content"), "212")

	if err := s.Canary(context.Background(), YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatal(err)
	}
	argv := string(raw)
	for _, want := range []string{"--simulate", "--print", "--js-runtimes", "deno", "--remote-components", "ejs:github", "--cookies", YouTubeCanary.URL} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv = %q, want it to contain %q", argv, want)
		}
	}
}

func TestCanary_OkWhenYtDlpReportsADuration(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = withYtDlpScript(t, "#!/bin/sh\necho '212'\n")

	if err := s.Canary(context.Background(), YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}
}

func TestCanary_SoundCloudExactPreviewDurationIsDark(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = withYtDlpScript(t, "#!/bin/sh\necho '30'\n")

	err := s.Canary(context.Background(), SoundCloudCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error for a Go+ preview duration")
	}
	if err.Error() != "preview only" {
		t.Errorf("Canary() = %q, want %q", err.Error(), "preview only")
	}
}

func TestCanary_ThirtySecondsFromYouTubeIsNotAPreview(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = withYtDlpScript(t, "#!/bin/sh\necho '30'\n")

	if err := s.Canary(context.Background(), YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil: a 30s YouTube video is not a SoundCloud preview", err)
	}
}

func TestCanary_ErrorIsTheTrimmedRedactedFirstStderrLine(t *testing.T) {
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookieFile, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", cookieFile, "")
	s.binary = withYtDlpScript(t, "#!/bin/sh\n"+
		"echo \"ERROR: Sign in to confirm you're not a bot\" >&2\n"+
		"echo 'second line, should be dropped' >&2\n"+
		"exit 1\n")

	err := s.Canary(context.Background(), YouTubeCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error when yt-dlp exits non-zero")
	}
	if !strings.Contains(err.Error(), "Sign in to confirm you're not a bot") {
		t.Errorf("Canary() = %q, want the first stderr line", err.Error())
	}
	if strings.Contains(err.Error(), "second line") {
		t.Errorf("Canary() = %q, want only the first stderr line", err.Error())
	}
}

func TestCanary_ErrorRedactsTheCookieJarPath(t *testing.T) {
	dir := t.TempDir()
	cookieFile := filepath.Join(dir, "very-secret-cookies.txt")
	if err := os.WriteFile(cookieFile, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewYtDlpAudioSearcher("", cookieFile, "")
	s.binary = withYtDlpScript(t, "#!/bin/sh\n"+
		"echo \"ERROR: unable to open --cookies $2\" >&2\nexit 1\n")

	err := s.Canary(context.Background(), YouTubeCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error when yt-dlp exits non-zero")
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("Canary() = %q, leaked the cookie jar's temp path", err.Error())
	}
}

func TestCanary_UnreadableCookieJarErrorRedactsItsPath(t *testing.T) {
	jar := filepath.Join("/srv", "private-jar", "cookies.txt")
	s := NewYtDlpAudioSearcher("", jar, "")

	err := s.Canary(context.Background(), YouTubeCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error when the cookie jar can't be read")
	}
	if strings.Contains(err.Error(), "private-jar") {
		t.Errorf("Canary() = %q, leaked the cookie jar path", err.Error())
	}
}

func TestNewYtDlpAudioSearcher_DefaultsCanaryTimeoutToSixtySeconds(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	if s.canaryTimeout != 60*time.Second {
		t.Fatalf("canaryTimeout = %v, want 60s", s.canaryTimeout)
	}
}

func TestCanary_TimesOutRatherThanHangingForever(t *testing.T) {
	s := NewYtDlpAudioSearcher("", "", "")
	s.binary = filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(s.binary, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.canaryTimeout = 100 * time.Millisecond

	err := s.Canary(context.Background(), YouTubeCanary)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Canary() = %v, want a timed-out error for a probe that ran past its timeout", err)
	}
}

// withYtDlpScript writes an executable standing in for yt-dlp and returns its
// path, so a test can drive Canary's real exec path without touching PATH (a
// canary test must not accidentally exercise a real yt-dlp another test left
// on PATH).
func withYtDlpScript(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCanary_SelectsFormatsTheSameWayDownloadDoes(t *testing.T) {
	outDir := t.TempDir()
	s := NewYtDlpAudioSearcher("", "", "")
	var downloadArgs string
	s.binary, downloadArgs = formatArgRecorder(t, outDir, "")
	if _, err := s.Download(context.Background(), "https://youtube.com/watch?v=1", outDir); err != nil {
		t.Fatalf("Download() = %v, want nil", err)
	}

	var canaryArgs string
	s.binary, canaryArgs = formatArgRecorder(t, "", "212")
	if err := s.Canary(context.Background(), YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	if got, want := formatFlag(t, canaryArgs), formatFlag(t, downloadArgs); got != want {
		t.Errorf("canary -f %q, want the download's selector %q so a format outage shows up as dark", got, want)
	}
}
