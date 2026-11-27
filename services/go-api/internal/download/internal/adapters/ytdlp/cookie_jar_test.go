package ytdlp_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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

var sessionCookieNames = []string{
	"LOGIN_INFO", "SID", "HSID", "SSID", "APISID", "SAPISID",
	"__Secure-1PAPISID", "__Secure-3PAPISID", "__Secure-1PSID", "__Secure-3PSID", "__Secure-1PSIDTS",
}

func jarWith(host, generation string, names ...string) string {
	cookies := make([]string, 0, len(names))
	for _, name := range names {
		cookies = append(cookies, host+" "+name+"="+name+"-"+generation)
	}
	return netscapeJar(cookies...)
}

func signedInJar(generation string) string {
	return jarWith(".youtube.com", generation, append(slices.Clone(sessionCookieNames), "SIDCC")...)
}

func sessionCookiesWithout(dropped string) []string {
	return slices.DeleteFunc(slices.Clone(sessionCookieNames), func(name string) bool { return name == dropped })
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
	return canaryIn("", jar)
}

func canaryIn(environment, jar string) error {
	return download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep, download.WithYtDlpEnvironment(environment)).Canary(context.Background(), download.YouTubeCanary)
}

type runKind struct {
	name string
	run  func(t *testing.T, s *download.YouTubeAudioSearcher)
}

var everyRunKind = []runKind{
	{"search", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.SearchQueries(context.Background(), []string{"artist song"})
	}},
	{"download", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.Download(context.Background(), youTubeWatchURL, t.TempDir())
	}},
	{"preview", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.DownloadPreview(context.Background(), youTubeWatchURL, t.TempDir(), 30)
	}},
	{"inspect", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		s.MarkUnplayable(context.Background(), []download.AudioCandidate{downloadtest.Candidate("https://soundcloud.com/artist/track")})
	}},
	{"canary", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_ = s.Canary(context.Background(), download.YouTubeCanary)
	}},
}

func TestCookieJar_EveryKindOfRunKeepsTheCookiesYtDlpRenewed(t *testing.T) {
	for _, kind := range everyRunKind {
		t.Run(kind.name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, signedInJar("new"), "exit 0")

			kind.run(t, download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep))

			if got := jarContents(t, jar); got != signedInJar("new") {
				t.Errorf("live jar after a successful %s = %q, want the renewed cookies", kind.name, got)
			}
		})
	}
}

func TestCookieJar_EveryKindOfRunRecordsItsFirstSignInFailure(t *testing.T) {
	for _, kind := range everyRunKind {
		t.Run(kind.name, func(t *testing.T) {
			logs := logging.CaptureJSON(t, slog.LevelDebug)
			jar := liveJar(t, signedInJar("old"))
			ytDlpFailing(t, botCheck)

			kind.run(t, download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep))

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

func TestCookieJar_ARenewalThatDropsAnySessionCookieIsNotKeptAndSaysWhichOne(t *testing.T) {
	for _, dropped := range sessionCookieNames {
		t.Run(dropped, func(t *testing.T) {
			logs := logging.CaptureJSON(t, slog.LevelDebug)
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, jarWith(".youtube.com", "new", append(sessionCookiesWithout(dropped), "SIDCC")...), "exit 0")

			_ = canaryWith(jar)

			if got := jarContents(t, jar); got != signedInJar("old") {
				t.Errorf("live jar = %q, want the signed-in jar kept over a renewal without %s", got, dropped)
			}
			assertOneRefusal(t, logs, dropped)
		})
	}
}

func TestCookieJar_ARenewalThatSignsOutNamesEveryMissingSessionCookie(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	jar := liveJar(t, signedInJar("old"))
	ytDlpRenewing(t, jarWith(".youtube.com", "new", "SIDCC"), "exit 0")

	_ = canaryWith(jar)

	assertOneRefusal(t, logs, sessionCookieNames...)
}

func assertOneRefusal(t *testing.T, logs *logging.Capture, missing ...string) {
	t.Helper()
	records := logRecords(t, logs, "acquisition.cookie_renewal_refused")
	if len(records) != 1 {
		t.Fatalf("renewal refusals = %v, want exactly one", records)
	}
	got, want := fmt.Sprint(records[0]["missing_cookies"]), fmt.Sprint(missing)
	if got != want || records[0]["level"] != "INFO" || records[0]["run"] != "canary" {
		t.Errorf("refusal = %v, want INFO for run canary with missing_cookies %s", records[0], want)
	}
}

func TestCookieJar_ASessionCookieOnAnotherHostDoesNotCount(t *testing.T) {
	withoutLoginInfo := jarWith(".youtube.com", "new", sessionCookiesWithout("LOGIN_INFO")...)
	cases := map[string]string{
		"login info moved to google":   withoutLoginInfo + jarWith(".google.com", "new", "LOGIN_INFO"),
		"login info on a lookalike":    withoutLoginInfo + jarWith(".notyoutube.com", "new", "LOGIN_INFO"),
		"login info line has no value": withoutLoginInfo + ".youtube.com\tTRUE\t/\tTRUE\t1999999999\tLOGIN_INFO",
	}
	for name, renewed := range cases {
		t.Run(name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			logs := logging.CaptureJSON(t, slog.LevelDebug)
			ytDlpRenewing(t, renewed, "exit 0")

			_ = canaryWith(jar)

			if got := jarContents(t, jar); got != signedInJar("old") {
				t.Errorf("live jar = %q, want the signed-in jar kept", got)
			}
			assertOneRefusal(t, logs, "LOGIN_INFO")
		})
	}
}

func TestCookieJar_EveryFormOfAYouTubeCookieKeepsTheSession(t *testing.T) {
	cases := map[string]string{
		"www host":             jarWith("www.youtube.com", "new", sessionCookieNames...),
		"bare host":            jarWith("youtube.com", "new", sessionCookieNames...),
		"http-only cookies":    jarWith("#HttpOnly_.youtube.com", "new", sessionCookieNames...),
		"windows line endings": strings.ReplaceAll(signedInJar("new"), "\n", "\r\n"),
	}
	for name, renewed := range cases {
		t.Run(name, func(t *testing.T) {
			logs := logging.CaptureJSON(t, slog.LevelDebug)
			jar := liveJar(t, signedInJar("old"))
			ytDlpRenewing(t, renewed, "exit 0")

			_ = canaryWith(jar)

			if got := jarContents(t, jar); got != renewed {
				t.Errorf("live jar = %q, want the renewal %q that kept every session cookie", got, renewed)
			}
			if refused := logRecords(t, logs, "acquisition.cookie_renewal_refused"); len(refused) != 0 {
				t.Errorf("renewal refusals = %v, want none for a renewal that kept every session cookie", refused)
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
	ytDlpReplacingTheLiveJar(t, jar, signedInJar("hum"), cookieArgLoop+"printf '%s' '"+signedInJar("new")+"' > \"$jar\"\necho 212")

	if err := canaryWith(jar); err != nil {
		t.Fatalf("Canary() = %v, want nil", err)
	}

	if got := jarContents(t, jar); got != signedInJar("hum") {
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
	logs := logging.CaptureJSON(t, slog.LevelDebug)
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
	logs := logging.CaptureJSON(t, slog.LevelDebug)
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

func firstSignInFailures(t *testing.T, logs *logging.Capture) []map[string]any {
	t.Helper()
	return logRecords(t, logs, "acquisition.cookie_session_first_signin_failure")
}

func logRecords(t *testing.T, logs *logging.Capture, msg string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.Lines(logs.String()) {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if record["msg"] == msg {
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

type recordedFailure struct {
	At              time.Time `json:"at"`
	SessionAgeHours float64   `json:"session_age_hours"`
	Run             string    `json:"run"`
	WriteBacks      int       `json:"write_backs"`
	Environment     string    `json:"environment"`
}

func firstFailureOnRecord(t *testing.T, jar string) *recordedFailure {
	t.Helper()
	raw, err := os.ReadFile(jar + ".session.json")
	if err != nil {
		t.Fatalf("session file: %v", err)
	}
	var session struct {
		FirstSignInFailure *recordedFailure `json:"first_signin_failure"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("session file %q is not JSON: %v", raw, err)
	}
	return session.FirstSignInFailure
}

func TestCookieJar_TheFirstSignInFailureIsLoggedAndKeptInTheSessionFileWithItsEnvironment(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	installed := time.Now().Add(-50 * time.Hour)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	ytDlpFailing(t, botCheck)
	before := time.Now()

	_ = canaryIn("production", jar)

	after := time.Now()
	records := firstSignInFailures(t, logs)
	assertOneFirstFailure(t, records, 50, installed, 0)
	if records[0]["environment"] != "production" || records[0]["run"] != "canary" {
		t.Errorf("logged failure = %v, want environment production and run canary", records[0])
	}
	got := firstFailureOnRecord(t, jar)
	if got == nil || got.SessionAgeHours != 50 || got.Run != "canary" || got.WriteBacks != 0 || got.Environment != "production" ||
		got.At.Before(before) || got.At.After(after) {
		t.Errorf("first failure on record = %+v, want 50h, canary, 0 write-backs, production, between %v and %v", got, before, after)
	}
}

func TestCookieJar_AFailureStagingCaughtFirstStaysOnRecordWhenProductionHitsItToo(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	jar := liveJarAt(t, signedInJar("old"), 0o644, time.Now().Add(-50*time.Hour))
	ytDlpFailing(t, botCheck)

	_ = canaryIn("staging", jar)
	caught := firstFailureOnRecord(t, jar)
	_ = canaryIn("production", jar)

	if records := firstSignInFailures(t, logs); len(records) != 1 || records[0]["environment"] != "staging" {
		t.Errorf("logged failures = %v, want only staging's", records)
	}
	if got := firstFailureOnRecord(t, jar); caught == nil || got == nil || *got != *caught || got.Environment != "staging" {
		t.Errorf("first failure on record = %+v, want staging's %+v kept", got, caught)
	}
}

func TestCookieJar_AFirstFailureIsLoggedOnlyOnceItIsOnRecord(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	jar := liveJar(t, signedInJar("old"))
	if err := os.MkdirAll(filepath.Join(jar+".session.json", "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	ytDlpFailing(t, botCheck)

	_ = canaryWith(jar)

	if records := firstSignInFailures(t, logs); len(records) != 0 {
		t.Errorf("first sign-in failure records = %v, want none while the session file cannot be written", records)
	}
	if warned := loggedErrors(t, logs, "acquisition.cookie_jar_update_failed"); len(warned) != 1 {
		t.Errorf("update failure warnings = %q, want one", warned)
	}
}

func TestCookieJar_TheFirstSignInFailureLogsTheSessionAgeSinceTheJarWasDroppedIn(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	installed := time.Now().Add(-50 * time.Hour)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	ytDlpFailing(t, botCheck)

	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 50, installed, 0)
}

func TestCookieJar_OnlyTheFirstSignInFailureOfAJarIsLoggedEvenAcrossRestarts(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	installed := time.Now().Add(-50 * time.Hour)
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	ytDlpFailing(t, botCheck)

	_ = canaryWith(jar)
	_ = canaryWith(jar)

	assertOneFirstFailure(t, firstSignInFailures(t, logs), 50, installed, 0)
}

func TestCookieJar_TheSessionAgeCountsFromTheDropInNotFromTheLastRenewal(t *testing.T) {
	logs := logging.CaptureJSON(t, slog.LevelDebug)
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

	logs := logging.CaptureJSON(t, slog.LevelDebug)
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
			logs := logging.CaptureJSON(t, slog.LevelDebug)
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
	logs := logging.CaptureJSON(t, slog.LevelDebug)
	jar := liveJarAt(t, signedInJar("old"), 0o644, time.Now().Add(-50*time.Hour))
	ytDlpReplacingTheLiveJar(t, jar, signedInJar("hum"), "echo \""+botCheck+"\" >&2\nexit 1")
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

const waitLimit = 5 * time.Second

func waitFor(t *testing.T, what string, done <-chan error, ready func() bool) {
	t.Helper()
	polltest.Until(t, waitLimit, what, func() bool {
		select {
		case err := <-done:
			t.Fatalf("the run returned %v before %s", err, what)
		default:
		}
		return ready()
	})
}

func returnedWithin(t *testing.T, limit time.Duration, what string, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("%s did not return within %v", what, limit)
		return nil
	}
}

func TestCookieJar_TheCopyYtDlpGetsLivesWhereTheTempSweeperReapsIt(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	jar := liveJar(t, signedInJar("old"))
	signals := t.TempDir()
	used, started, proceed := filepath.Join(signals, "used"), filepath.Join(signals, "started"), filepath.Join(signals, "proceed")
	installYtDlp(t, cookieArgLoop+"printf '%s' \"$jar\" > '"+used+"'\n: > '"+started+"'\n"+
		"while [ ! -f '"+proceed+"' ]; do sleep 0.01; done\necho 212")
	done := make(chan error, 1)

	go func() { done <- canaryWith(jar) }()
	waitFor(t, "yt-dlp to start", done, func() bool { _, err := os.Stat(started); return err == nil })
	swept := download.NewTempWorkspace().SweepAbandoned(context.Background(), time.Now().Add(time.Hour))
	if err := os.WriteFile(proceed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = returnedWithin(t, waitLimit, "Canary()", done)

	copyPath, err := os.ReadFile(used)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Dir(string(copyPath))); swept != 1 || !os.IsNotExist(statErr) {
		t.Errorf("swept %d dirs, copy dir stat err %v, want the copy's dir reaped as abandoned", swept, statErr)
	}
}

func signedOutSince(jar string) (time.Time, bool) {
	return download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep).SignedOutSince(context.Background())
}

func signedOutJar(t *testing.T) (jar string, since time.Time) {
	t.Helper()
	jar = liveJar(t, signedInJar("old"))
	ytDlpFailing(t, botCheck)
	_ = canaryWith(jar)
	since, signedOut := signedOutSince(jar)
	if !signedOut {
		t.Fatal("a bot check did not mark the jar signed out")
	}
	return jar, since
}

func TestCookieJar_EveryKindOfRunThatHitsTheBotCheckMarksTheJarSignedOutFromThatMoment(t *testing.T) {
	for _, kind := range everyRunKind {
		t.Run(kind.name, func(t *testing.T) {
			jar := liveJar(t, signedInJar("old"))
			ytDlpFailing(t, botCheck)
			before := time.Now()

			kind.run(t, download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep))

			since, signedOut := signedOutSince(jar)
			if !signedOut || since.Before(before) || since.After(time.Now()) {
				t.Errorf("signed out since %v (%v) after a bot-checked %s, want signed out from a moment after %v", since, signedOut, kind.name, before)
			}
		})
	}
}

func TestCookieJar_OtherFailuresDoNotMarkTheJarSignedOut(t *testing.T) {
	jar := liveJar(t, signedInJar("old"))
	ytDlpFailing(t, "ERROR: [youtube] jNQXAC9IVRw: Video unavailable")

	_ = canaryWith(jar)

	if since, signedOut := signedOutSince(jar); signedOut {
		t.Errorf("signed out since %v after an unavailable video, want signed in", since)
	}
}

func TestCookieJar_LaterBotChecksKeepTheMomentTheJarWasFirstSignedOut(t *testing.T) {
	jar, first := signedOutJar(t)

	_ = canaryIn("production", jar)

	if since, signedOut := signedOutSince(jar); !signedOut || !since.Equal(first) {
		t.Errorf("signed out since %v (%v) after a second bot check, want the first one's %v", since, signedOut, first)
	}
}

var youTubeRunKinds = []runKind{
	{"youtube canary", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_ = s.Canary(context.Background(), download.YouTubeCanary)
	}},
	{"ytmusic canary", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_ = s.Canary(context.Background(), download.YTMusicCanary)
	}},
	{"youtube download", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.Download(context.Background(), youTubeWatchURL, t.TempDir())
	}},
	{"mixed-case host download", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.Download(context.Background(), "https://WWW.YouTube.com/watch?v=jNQXAC9IVRw", t.TempDir())
	}},
	{"youtu.be download", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.Download(context.Background(), "https://youtu.be/jNQXAC9IVRw", t.TempDir())
	}},
	{"search", func(t *testing.T, s *download.YouTubeAudioSearcher) {
		_, _ = s.SearchQueries(context.Background(), []string{"artist song"})
	}},
}

func TestCookieJar_AYouTubeRunThatSucceedsSignsTheJarBackIn(t *testing.T) {
	for _, kind := range youTubeRunKinds {
		for name, succeed := range map[string]func(t *testing.T){
			"cookies unchanged": func(t *testing.T) { installYtDlp(t, "exit 0") },
			"cookies renewed":   func(t *testing.T) { ytDlpRenewing(t, signedInJar("new"), "exit 0") },
		} {
			t.Run(kind.name+"/"+name, func(t *testing.T) {
				jar, _ := signedOutJar(t)
				succeed(t)

				kind.run(t, download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep))

				if since, signedOut := signedOutSince(jar); signedOut {
					t.Errorf("still signed out since %v after a %s that succeeded", since, kind.name)
				}
			})
		}
	}
}

func TestCookieJar_ARunThatSucceedsOffYouTubeLeavesTheJarSignedOut(t *testing.T) {
	for _, kind := range []runKind{
		{"soundcloud canary", func(t *testing.T, s *download.YouTubeAudioSearcher) {
			_ = s.Canary(context.Background(), download.SoundCloudCanary)
		}},
		{"soundcloud download", func(t *testing.T, s *download.YouTubeAudioSearcher) {
			_, _ = s.Download(context.Background(), "https://soundcloud.com/artist/track", t.TempDir())
		}},
		{"soundcloud inspect", func(t *testing.T, s *download.YouTubeAudioSearcher) {
			s.MarkUnplayable(context.Background(), []download.AudioCandidate{downloadtest.Candidate("https://soundcloud.com/artist/track")})
		}},
		{"lookalike host", func(t *testing.T, s *download.YouTubeAudioSearcher) {
			_, _ = s.Download(context.Background(), "https://notyoutube.com/watch?v=jNQXAC9IVRw", t.TempDir())
		}},
	} {
		for name, succeed := range map[string]func(t *testing.T){
			"cookies unchanged": func(t *testing.T) { installYtDlp(t, "exit 0") },
			"cookies renewed":   func(t *testing.T) { ytDlpRenewing(t, signedInJar("new"), "exit 0") },
		} {
			t.Run(kind.name+"/"+name, func(t *testing.T) {
				jar, first := signedOutJar(t)
				succeed(t)

				kind.run(t, download.MustNewYtDlpAudioSearcher("", jar, "", time.Now, clock.Sleep))

				if since, signedOut := signedOutSince(jar); !signedOut || !since.Equal(first) {
					t.Errorf("signed out since %v (%v) after a %s, want still signed out since %v", since, signedOut, kind.name, first)
				}
			})
		}
	}
}

func TestCookieJar_AJarDroppedInAfterASignOutIsSignedIn(t *testing.T) {
	jar, _ := signedOutJar(t)

	if err := os.WriteFile(jar, []byte(signedInJar("human")), 0o644); err != nil {
		t.Fatal(err)
	}

	if since, signedOut := signedOutSince(jar); signedOut {
		t.Errorf("signed out since %v right after a new jar was dropped in, want signed in", since)
	}
}

func TestCookieJar_WithNoJarToReadNothingIsSignedOut(t *testing.T) {
	for name, jar := range map[string]string{
		"no jar configured": "",
		"jar missing":       filepath.Join(t.TempDir(), "cookies.txt"),
	} {
		t.Run(name, func(t *testing.T) {
			if since, signedOut := signedOutSince(jar); signedOut {
				t.Errorf("signed out since %v, want signed in", since)
			}
		})
	}
}

func ytDlpOverwritingTheSession(t *testing.T, live, jar string, session map[string]any, after string) {
	t.Helper()
	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	jarSource, sessionSource := filepath.Join(dir, "jar.txt"), filepath.Join(dir, "session.json")
	if err := os.WriteFile(jarSource, []byte(jar), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionSource, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	installYtDlp(t, "cp '"+jarSource+"' '"+live+"'\ncp '"+sessionSource+"' '"+live+".session.json'\n"+after)
}

func jarDigest(jar string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(jar)))
}

func TestCookieJar_ASuccessOnAJarThatWasReplacedLeavesTheNewJarSignedOut(t *testing.T) {
	jar, _ := signedOutJar(t)
	newJarSignedOut := time.Date(2026, 11, 20, 14, 30, 0, 0, time.UTC)
	ytDlpOverwritingTheSession(t, jar, signedInJar("human"), map[string]any{
		"jar_sha256":       jarDigest(signedInJar("human")),
		"installed_at":     newJarSignedOut.Add(-time.Hour),
		"signed_out_since": newJarSignedOut,
	}, "exit 0")

	_ = canaryWith(jar)

	if since, signedOut := signedOutSince(jar); !signedOut || !since.Equal(newJarSignedOut) {
		t.Errorf("signed out since %v (%v) after the old jar's run succeeded, want the new jar still signed out since %v", since, signedOut, newJarSignedOut)
	}
}

func TestCookieJar_ABotCheckWhileAnotherRunKeptRenewedCookiesStillSignsTheJarOut(t *testing.T) {
	installed := time.Now().Add(-50 * time.Hour).Truncate(time.Second).UTC()
	jar := liveJarAt(t, signedInJar("old"), 0o644, installed)
	ytDlpOverwritingTheSession(t, jar, signedInJar("renewed"), map[string]any{
		"jar_sha256":   jarDigest(signedInJar("renewed")),
		"installed_at": installed,
		"write_backs":  1,
	}, "echo \""+botCheck+"\" >&2\nexit 1")

	_ = canaryWith(jar)

	if since, signedOut := signedOutSince(jar); !signedOut {
		t.Errorf("signed in after a bot check while another run kept renewed cookies, want signed out (since %v)", since)
	}
}
