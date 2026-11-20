package ytdlp_test

import (
	"altune/go-api/internal/acquisition"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const botCheck = "ERROR: [youtube] jNQXAC9IVRw: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication."

func netscapeJar(cookies ...string) string {
	jar := "# Netscape HTTP Cookie File\n\n"
	for _, cookie := range cookies {
		domain, nameValue, _ := strings.Cut(cookie, " ")
		name, value, _ := strings.Cut(nameValue, "=")
		jar += strings.Join([]string{domain, "TRUE", "/", "TRUE", "1999999999", name, value}, "\t") + "\n"
	}
	return jar
}

func signedInJar(generation string) string {
	return netscapeJar(".youtube.com LOGIN_INFO=login-"+generation, ".youtube.com SAPISID=sapisid-"+generation, ".youtube.com SIDCC=sidcc-"+generation)
}

func liveJarAt(t *testing.T, contents string, mode fs.FileMode, installed time.Time) string {
	t.Helper()
	jar := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(jar, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(jar, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(jar, installed, installed); err != nil {
		t.Fatal(err)
	}
	return jar
}

func liveJar(t *testing.T, contents string) string {
	t.Helper()
	return liveJarAt(t, contents, 0o644, time.Now().Add(-time.Hour))
}

func jarContents(t *testing.T, jar string) string {
	t.Helper()
	data, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const cookieArgLoop = "prev=\"\"\nfor arg in \"$@\"; do\n  if [ \"$prev\" = \"--cookies\" ]; then jar=\"$arg\"; fi\n  prev=\"$arg\"\ndone\n"

func ytDlpRenewing(t *testing.T, renewed string, after string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "renewed.txt")
	if err := os.WriteFile(source, []byte(renewed), 0o600); err != nil {
		t.Fatal(err)
	}
	installYtDlp(t, cookieArgLoop+"cp '"+source+"' \"$jar\"\n"+after)
}

func ytDlpFailing(t *testing.T, stderrLine string) {
	t.Helper()
	installYtDlp(t, "echo \""+stderrLine+"\" >&2\nexit 1")
}

func ytDlpReplacingTheLiveJar(t *testing.T, live, replacement, after string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "replacement.txt")
	if err := os.WriteFile(source, []byte(replacement), 0o644); err != nil {
		t.Fatal(err)
	}
	installYtDlp(t, cookieArgLoop+"cp '"+source+"' '"+live+"'\n"+after)
}

func canaryWith(jar string) error {
	return acquisition.NewYtDlpAudioSearcher("", jar, "").Canary(context.Background(), acquisition.YouTubeCanary)
}

type runKind struct {
	name string
	run  func(t *testing.T, s *acquisition.YtDlpAudioSearcher)
}

var everyRunKind = []runKind{
	{"search", func(t *testing.T, s *acquisition.YtDlpAudioSearcher) {
		_, _ = s.SearchQueries(context.Background(), []string{"artist song"})
	}},
	{"download", func(t *testing.T, s *acquisition.YtDlpAudioSearcher) {
		_, _ = s.Download(context.Background(), youTubeWatchURL, t.TempDir())
	}},
	{"preview", func(t *testing.T, s *acquisition.YtDlpAudioSearcher) {
		_, _ = s.DownloadPreview(context.Background(), youTubeWatchURL, t.TempDir(), 30)
	}},
	{"inspect", func(t *testing.T, s *acquisition.YtDlpAudioSearcher) {
		s.MarkUnplayable(context.Background(), []acquisition.AudioCandidate{{URL: "https://soundcloud.com/artist/track"}})
	}},
	{"canary", func(t *testing.T, s *acquisition.YtDlpAudioSearcher) {
		_ = s.Canary(context.Background(), acquisition.YouTubeCanary)
	}},
}

func TestCookieJar_EveryKindOfRunKeepsTheCookiesYtDlpRenewed(t *testing.T) {
	for _, kind := range everyRunKind {
		t.Run(kind.name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, signedInJar("new"), "exit 0")

			kind.run(t, acquisition.NewYtDlpAudioSearcher("", jar, ""))

			if got := jarContents(t, jar); got != signedInJar("new") {
				t.Errorf("live jar after a successful %s = %q, want the renewed cookies", kind.name, got)
			}
		})
	}
}

func TestCookieJar_EveryKindOfRunRecordsItsFirstSignInFailure(t *testing.T) {
	for _, kind := range everyRunKind {
		t.Run(kind.name, func(t *testing.T) {
			logs := captureLogs(t)
			jar := liveJar(t, signedInJar("old"))
			ytDlpFailing(t, botCheck)

			kind.run(t, acquisition.NewYtDlpAudioSearcher("", jar, ""))

			records := firstSignInFailures(t, logs)
			if len(records) != 1 || records[0]["run"] != kind.name {
				t.Errorf("first sign-in failure records = %v, want one naming run %q", records, kind.name)
			}
		})
	}
}

func TestCookieJar_AFailedRunLeavesTheLiveJarAsItWas(t *testing.T) {
	for name, stderr := range map[string]string{"bot check": botCheck, "other failure": "ERROR: Video unavailable"} {
		t.Run(name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, signedInJar("new"), "echo \""+stderr+"\" >&2\nexit 1")

			_ = canaryWith(jar)

			if got := jarContents(t, jar); got != signedInJar("old") {
				t.Errorf("live jar after a failed run = %q, want it untouched", got)
			}
		})
	}
}

func TestCookieJar_ARenewalThatSignsTheSessionOutIsNotKept(t *testing.T) {
	cases := map[string]string{
		"login info cleared":           netscapeJar(".youtube.com SAPISID=s", ".youtube.com SIDCC=new"),
		"every sapisid cookie cleared": netscapeJar(".youtube.com LOGIN_INFO=l", ".youtube.com SIDCC=new"),
		"login info only on google":    netscapeJar(".google.com LOGIN_INFO=l", ".youtube.com SAPISID=s"),
		"sapisid only on google":       netscapeJar(".youtube.com LOGIN_INFO=l", ".google.com SAPISID=s"),
		"lookalike youtube host":       netscapeJar(".notyoutube.com LOGIN_INFO=l", ".youtube.com SAPISID=s"),
		"truncated cookie line":        netscapeJar(".youtube.com SAPISID=s") + ".youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO",
	}
	for name, renewed := range cases {
		t.Run(name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, renewed, "exit 0")

			_ = canaryWith(jar)

			if got := jarContents(t, jar); got != signedInJar("old") {
				t.Errorf("live jar = %q, want the signed-in jar kept over a signed-out renewal", got)
			}
		})
	}
}

func TestCookieJar_AnySignedInRenewalIsKept(t *testing.T) {
	cases := map[string]string{
		"SAPISID":              netscapeJar(".youtube.com LOGIN_INFO=l", ".youtube.com SAPISID=s"),
		"__Secure-1PAPISID":    netscapeJar(".youtube.com LOGIN_INFO=l", ".youtube.com __Secure-1PAPISID=s"),
		"__Secure-3PAPISID":    netscapeJar(".youtube.com LOGIN_INFO=l", ".youtube.com __Secure-3PAPISID=s"),
		"www host":             netscapeJar("www.youtube.com LOGIN_INFO=l", "www.youtube.com SAPISID=s"),
		"bare host":            netscapeJar("youtube.com LOGIN_INFO=l", "youtube.com SAPISID=s"),
		"http-only login info": netscapeJar("#HttpOnly_.youtube.com LOGIN_INFO=l", ".youtube.com SAPISID=s"),
		"windows line endings": strings.ReplaceAll(netscapeJar(".youtube.com LOGIN_INFO=l", ".youtube.com SAPISID=s"), "\n", "\r\n"),
	}
	for name, renewed := range cases {
		t.Run(name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, renewed, "exit 0")

			_ = canaryWith(jar)

			if got := jarContents(t, jar); got != renewed {
				t.Errorf("live jar = %q, want the signed-in renewal %q", got, renewed)
			}
		})
	}
}

func TestCookieJar_AnAnonymousJarIsRenewedToo(t *testing.T) {
	jar := liveJar(t, netscapeJar(".youtube.com VISITOR_INFO1_LIVE=old"))
	renewed := netscapeJar(".youtube.com VISITOR_INFO1_LIVE=new")
	ytDlpRenewing(t, renewed, "exit 0")

	_ = canaryWith(jar)

	if got := jarContents(t, jar); got != renewed {
		t.Errorf("live jar = %q, want the renewed anonymous jar", got)
	}
}

func TestCookieJar_AJarReplacedWhileYtDlpRanIsNotOverwritten(t *testing.T) {
	jar := liveJar(t, signedInJar("old"))
	ytDlpReplacingTheLiveJar(t, jar, signedInJar("human"), cookieArgLoop+"echo '"+signedInJar("new")+"' > \"$jar\"\necho 212")

	if err := canaryWith(jar); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	if got := jarContents(t, jar); got != signedInJar("human") {
		t.Errorf("live jar = %q, want the jar dropped in during the run kept", got)
	}
}

func TestCookieJar_TheKeptJarKeepsTheLiveJarsPermissions(t *testing.T) {
	jar := liveJarAt(t, signedInJar("old"), 0o666, time.Now())
	ytDlpRenewing(t, signedInJar("new"), "exit 0")

	_ = canaryWith(jar)

	info, err := os.Stat(jar)
	if err != nil {
		t.Fatal(err)
	}
	if jarContents(t, jar) != signedInJar("new") || info.Mode().Perm() != 0o666 {
		t.Errorf("live jar mode = %v after a write-back, want the live jar's 0666 kept so the person who drops it in can still read and replace it", info.Mode().Perm())
	}
}

func TestCookieJar_AHalfWrittenReplacementLeftByACrashDoesNotBlockTheNextWriteBack(t *testing.T) {
	jar := liveJarAt(t, signedInJar("old"), 0o444, time.Now())
	if err := os.WriteFile(jar+".next", []byte("# half"), 0o444); err != nil {
		t.Fatal(err)
	}
	ytDlpRenewing(t, signedInJar("new"), "exit 0")

	_ = canaryWith(jar)

	if got := jarContents(t, jar); got != signedInJar("new") {
		t.Errorf("live jar = %q, want the renewed cookies despite the leftover replacement", got)
	}
}

func TestCookieJar_CookiesYtDlpLeftAsTheyWereDoNotRewriteTheLiveJar(t *testing.T) {
	installed := time.Now().Add(-50 * time.Hour).Truncate(time.Second)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	installYtDlp(t, "echo 212")

	if err := canaryWith(jar); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	info, err := os.Stat(jar)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(installed) {
		t.Errorf("live jar mtime = %v, want %v left alone when yt-dlp renewed nothing", info.ModTime(), installed)
	}
}

func TestCookieJar_AJarThatCannotBeUpdatedWarnsAndTheRunStillSucceeds(t *testing.T) {
	logs := captureLogs(t)
	jar := liveJar(t, signedInJar("old"))
	if err := os.Mkdir(jar+".lock", 0o755); err != nil {
		t.Fatal(err)
	}
	ytDlpRenewing(t, signedInJar("new"), "echo 212")

	if err := canaryWith(jar); err != nil {
		t.Fatalf("Canary() = %v, want nil: a jar that cannot be updated must not fail the run", err)
	}

	if got := jarContents(t, jar); got != signedInJar("old") {
		t.Errorf("live jar = %q, want it untouched", got)
	}
	if warned := loggedErrors(t, logs, "acquisition.cookie_jar_update_failed"); len(warned) != 1 || strings.Contains(warned[0], jar) {
		t.Errorf("update failure warnings = %q, want one that does not name the jar path", warned)
	}
}

func TestCookieJar_WithNoJarConfiguredNothingIsCopiedOrKept(t *testing.T) {
	logs := captureLogs(t)
	calls := installYtDlp(t, "echo 212")

	if err := canaryWith(""); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	if argv := onlyCall(t, calls); strings.Contains(strings.Join(argv, " "), "--cookies") {
		t.Errorf("argv = %q, want no --cookies without a jar", argv)
	}
	if warned := loggedErrors(t, logs, "acquisition.cookie_jar_update_failed"); len(warned) != 0 {
		t.Errorf("update failure warnings = %q, want none without a jar", warned)
	}
}

func firstSignInFailures(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.Lines(logs.String()) {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if record["msg"] == "acquisition.cookie_session_first_signin_failure" {
			records = append(records, record)
		}
	}
	return records
}

func assertOneFirstFailure(t *testing.T, records []map[string]any, ageHours float64, installed time.Time, writeBacks float64) {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("first sign-in failure records = %v, want exactly one", records)
	}
	got := records[0]
	if got["session_age_hours"] != ageHours || got["installed_at"] != installed.UTC().Format(time.RFC3339) || got["write_backs"] != writeBacks || got["level"] != "WARN" {
		t.Errorf("record = %v, want WARN with session_age_hours %v, installed_at %s, write_backs %v", got, ageHours, installed.UTC().Format(time.RFC3339), writeBacks)
	}
}

func TestCookieJar_TheFirstSignInFailureLogsTheSessionAgeSinceTheJarWasDroppedIn(t *testing.T) {
	logs := captureLogs(t)
	installed := time.Now().Add(-50 * time.Hour)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	ytDlpFailing(t, botCheck)

	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 50, installed, 0)
}

func TestCookieJar_OnlyTheFirstSignInFailureOfAJarIsLoggedEvenAcrossRestarts(t *testing.T) {
	logs := captureLogs(t)
	installed := time.Now().Add(-50 * time.Hour)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	ytDlpFailing(t, botCheck)

	_ = canaryWith(jar)
	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 50, installed, 0)
}

func TestCookieJar_TheSessionAgeCountsFromTheDropInNotFromTheLastRenewal(t *testing.T) {
	logs := captureLogs(t)
	installed := time.Now().Add(-50 * time.Hour)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)

	ytDlpRenewing(t, signedInJar("new"), "exit 0")
	_ = canaryWith(jar)
	ytDlpRenewing(t, signedInJar("newer"), "exit 0")
	_ = canaryWith(jar)
	ytDlpFailing(t, botCheck)
	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 50, installed, 2)
}

func TestCookieJar_AJarDroppedInAfterAFailureStartsANewSession(t *testing.T) {
	jar := liveJarAt(t, signedInJar("old"), 0o644, time.Now().Add(-50*time.Hour))
	ytDlpRenewing(t, signedInJar("new"), "exit 0")
	_ = canaryWith(jar)
	ytDlpFailing(t, botCheck)
	_ = canaryWith(jar)

	logs := captureLogs(t)
	dropped := time.Now().Add(-150 * time.Minute)
	if err := os.WriteFile(jar, []byte(signedInJar("human")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(jar, dropped, dropped); err != nil {
		t.Fatal(err)
	}
	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 2.5, dropped, 0)
}

func TestCookieJar_OnlyTheBotCheckCountsAsASignInFailure(t *testing.T) {
	for _, stderr := range []string{
		"ERROR: [youtube] jNQXAC9IVRw: Video unavailable",
		"ERROR: [youtube] jNQXAC9IVRw: Sign in to confirm your age. This video may be inappropriate for some users.",
	} {
		t.Run(stderr, func(t *testing.T) {
			logs := captureLogs(t)
			jar := liveJar(t, signedInJar("old"))
			ytDlpFailing(t, stderr)

			_ = canaryWith(jar)

			if records := firstSignInFailures(t, logs); len(records) != 0 {
				t.Errorf("first sign-in failure records = %v, want none for %q", records, stderr)
			}
		})
	}
}

func TestCookieJar_ABotCheckOnAJarReplacedMidRunIsNotChargedToTheNewJar(t *testing.T) {
	logs := captureLogs(t)
	jar := liveJarAt(t, signedInJar("old"), 0o644, time.Now().Add(-50*time.Hour))
	ytDlpReplacingTheLiveJar(t, jar, signedInJar("human"), "echo \""+botCheck+"\" >&2\nexit 1")
	_ = canaryWith(jar)
	if records := firstSignInFailures(t, logs); len(records) != 0 {
		t.Fatalf("first sign-in failure records = %v, want none: the run used the jar that was replaced", records)
	}

	dropped := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(jar, dropped, dropped); err != nil {
		t.Fatal(err)
	}
	ytDlpFailing(t, botCheck)
	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 3, dropped, 0)
}
