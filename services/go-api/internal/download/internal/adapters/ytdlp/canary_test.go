package ytdlp_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cookieJar(t *testing.T, name, contents string) string {
	t.Helper()
	jar := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(jar, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return jar
}

func TestCanary_UsesATempCopyOfTheCookieJarAndCleansItUp(t *testing.T) {
	jar := cookieJar(t, "cookies.txt", liveJarContents)
	capture := captureCookieJar(t, "echo '212'")

	if err := download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep).Canary(context.Background(), download.YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	capture.assertTempCopyUsedAndRemoved(t, jar)
}

func TestCanary_CarriesTheSameAuthFlagsAsSearchAndDownload(t *testing.T) {
	calls := captureCookieJar(t, "echo '212'").calls
	s := download.MustNewYtDlpAudioSearcher("", cookieJar(t, "cookies.txt", "jar"), "deno", time.Now, clock.Sleep)

	if err := s.Canary(context.Background(), download.YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	argv := strings.Join(onlyCall(t, calls), "\n")
	for _, want := range []string{"--simulate", "--print", "--js-runtimes", "deno", "--remote-components", "ejs:github", "--cookies", "https://music.youtube.com/watch?v=jNQXAC9IVRw"} {
		if !strings.Contains(argv, want) {
			t.Errorf("argv = %q, want it to contain %q", argv, want)
		}
	}
}

func TestCanary_OkWhenYtDlpReportsADuration(t *testing.T) {
	installYtDlp(t, "echo '212'")

	if err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), download.YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}
}

func TestCanary_SoundCloudExactPreviewDurationIsDark(t *testing.T) {
	installYtDlp(t, "echo '30'")

	err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), download.SoundCloudCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error for a Go+ preview duration")
	}
	if err.Error() != "preview only" {
		t.Errorf("Canary() = %q, want %q", err.Error(), "preview only")
	}
}

func TestCanary_ThirtySecondsFromYouTubeIsNotAPreview(t *testing.T) {
	installYtDlp(t, "echo '30'")

	if err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), download.YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil: a 30s YouTube video is not a SoundCloud preview", err)
	}
}

func TestCanary_ErrorIsTheTrimmedRedactedFirstStderrLine(t *testing.T) {
	installYtDlp(t, "echo \"ERROR: Sign in to confirm you're not a bot\" >&2\n"+
		"echo 'second line, should be dropped' >&2\n"+
		"exit 1")
	s := download.MustNewYtDlpAudioSearcher("", cookieJar(t, "cookies.txt", "jar"), "", time.Now, clock.Sleep)

	err := s.Canary(context.Background(), download.YouTubeCanary)
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
	installYtDlp(t, "echo \"ERROR: unable to open --cookies $2\" >&2\nexit 1")
	jar := cookieJar(t, "very-secret-cookies.txt", "jar")
	s := download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep)

	err := s.Canary(context.Background(), download.YouTubeCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error when yt-dlp exits non-zero")
	}
	if strings.Contains(err.Error(), os.TempDir()) {
		t.Errorf("Canary() = %q, leaked the cookie jar's temp path", err.Error())
	}
}

func TestCanary_UnreadableCookieJarErrorRedactsItsPath(t *testing.T) {
	jar := filepath.Join("/srv", "private-jar", "cookies.txt")
	s := download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep)

	err := s.Canary(context.Background(), download.YouTubeCanary)
	if err == nil {
		t.Fatal("Canary() = nil, want an error when the cookie jar can't be read")
	}
	if strings.Contains(err.Error(), "private-jar") {
		t.Errorf("Canary() = %q, leaked the cookie jar path", err.Error())
	}
}

func TestCanary_TimesOutRatherThanHangingForever(t *testing.T) {
	installYtDlp(t, "exec sleep 30")
	s := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).WithCanaryTimeout(100 * time.Millisecond)

	err := s.Canary(context.Background(), download.YouTubeCanary)

	if err == nil || err.Error() != "timed out after 100ms" {
		t.Fatalf("Canary() = %v, want a timed-out error naming the timeout for a probe that ran past it", err)
	}
}

func formatAsked(t *testing.T, calls string) string {
	t.Helper()
	return flagValue(t, onlyCall(t, calls), "-f")
}

func downloadFormat(t *testing.T) string {
	t.Helper()
	outDir := t.TempDir()
	calls := installYtDlp(t, stubDownload(outDir))
	if _, err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Download(context.Background(), youTubeWatchURL, outDir); err != nil {
		t.Fatalf("Download() = %v, want nil", err)
	}
	return formatAsked(t, calls)
}

func canaryFormat(t *testing.T, source download.CanarySource) string {
	t.Helper()
	calls := installYtDlp(t, "echo '212'")
	if err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), source); err != nil {
		t.Fatalf("Canary(%s) = %v, want nil", source.Name, err)
	}
	return formatAsked(t, calls)
}

func TestCanary_SelectsFormatsTheSameWayDownloadDoes(t *testing.T) {
	if got, want := canaryFormat(t, download.YouTubeCanary), downloadFormat(t); got != want {
		t.Errorf("canary -f %q, want the download's selector %q so a format outage shows up as dark", got, want)
	}
}

func videoOnlyYtDlp(t *testing.T) {
	t.Helper()
	installYtDlp(t, "prev=\"\"\n"+
		"for arg in \"$@\"; do\n"+
		"  if [ \"$prev\" = \"-f\" ]; then\n"+
		"    case \"$arg\" in\n"+
		"      */best\\[*) ;;\n"+
		"      *) echo 'ERROR: [youtube] jNQXAC9IVRw: Requested format is not available' >&2; exit 1 ;;\n"+
		"    esac\n"+
		"  fi\n"+
		"  prev=\"$arg\"\n"+
		"done\n"+
		"echo '19'")
}

func TestCanary_YTMusicIsDarkWhenOnlyVideoFormatsAreOffered(t *testing.T) {
	videoOnlyYtDlp(t)

	if err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), download.YTMusicCanary); err == nil {
		t.Fatal("Canary(ytmusic) = nil, want dark when no audio-only format is offered")
	}
}

func TestCanary_YouTubeStaysOkOnVideoFormatsBecauseDownloadsStillWork(t *testing.T) {
	videoOnlyYtDlp(t)

	if err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), download.YouTubeCanary); err != nil {
		t.Fatalf("Canary(youtube) = %v, want nil", err)
	}
}

func TestCanary_SoundCloudPreviewUsesTheInspectionSlack(t *testing.T) {
	cases := []struct {
		reported string
		wantDark bool
	}{
		{"30.04", true},
		{"29.98", true},
		{"30.5", true},
		{"30.6", false},
		{"31", false},
	}
	for _, tc := range cases {
		t.Run(tc.reported, func(t *testing.T) {
			installYtDlp(t, "echo '"+tc.reported+"'")

			err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(context.Background(), download.SoundCloudCanary)

			if tc.wantDark && (err == nil || err.Error() != "preview only") {
				t.Fatalf("Canary() = %v, want preview only for %s s", err, tc.reported)
			}
			if !tc.wantDark && err != nil {
				t.Fatalf("Canary() = %v, want nil for %s s", err, tc.reported)
			}
		})
	}
}

func TestCanary_ParentCancelIsNotATimeout(t *testing.T) {
	ctx := cancelOnceFileExists(t, installYtDlp(t, "exec sleep 30"))

	err := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Canary(ctx, download.SoundCloudCanary)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Canary() = %v, want context.Canceled", err)
	}
	if err != nil && strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Canary() = %q, must not call a parent cancel a timeout", err)
	}
}

func TestCanary_YTMusicAsksForTheAudioOnlyPrefixOfTheDownloadSelector(t *testing.T) {
	audioOnly, _, found := strings.Cut(downloadFormat(t), "/best[")
	if !found {
		t.Fatalf("download selector %q has no combined-format fallback to cut", downloadFormat(t))
	}

	if got := canaryFormat(t, download.YTMusicCanary); got != audioOnly {
		t.Errorf("canary -f %q, want the download selector audio-only prefix %q", got, audioOnly)
	}
}

func TestCanary_ANonPositiveTimeoutKeepsTheDefault(t *testing.T) {
	installYtDlp(t, "echo '212'")
	s := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).WithCanaryTimeout(0)

	if err := s.Canary(context.Background(), download.YouTubeCanary); err != nil {
		t.Fatalf("Canary() = %v, want nil: a zero timeout must leave the default in place", err)
	}
}
