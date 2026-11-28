package ytdlp_test

import (
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/polltest"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const youTubeWatchURL = "https://youtube.com/watch?v=1"

func cancelOnceFileExists(t *testing.T, path string) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer cancel()
		polltest.Within(10*time.Second, func() bool {
			if ctx.Err() != nil {
				return true
			}
			_, err := os.Stat(path)
			return err == nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	return ctx
}

func installYtDlp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := "#!/bin/sh\nargv=$(printf '%s\\t' \"$@\")\nprintf '%s\\n' \"$argv\" >> '" + calls + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func withoutAnyBinaries(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func recordedCalls(t *testing.T, calls string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("yt-dlp never ran: %v", err)
	}
	var argvs [][]string
	for line := range strings.Lines(string(raw)) {
		argvs = append(argvs, strings.Split(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\t"), "\t"))
	}
	return argvs
}

func onlyCall(t *testing.T, calls string) []string {
	t.Helper()
	argvs := recordedCalls(t, calls)
	if len(argvs) != 1 {
		t.Fatalf("yt-dlp ran %d times %q, want once", len(argvs), argvs)
	}
	return argvs[0]
}

func callWith(t *testing.T, calls, arg string) []string {
	t.Helper()
	for _, argv := range recordedCalls(t, calls) {
		if slices.Contains(argv, arg) {
			return argv
		}
	}
	t.Fatalf("no yt-dlp call carried %q", arg)
	return nil
}

func flagValue(t *testing.T, argv []string, flag string) string {
	t.Helper()
	i := slices.Index(argv, flag)
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("argv = %q, want a value after %s", argv, flag)
	}
	return argv[i+1]
}

func hasFlagValue(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

func ytDlpByMode(search, inspect, other string) string {
	return "mode=other\n" +
		"for a in \"$@\"; do case \"$a\" in --dump-json) mode=search ;; -J) mode=inspect ;; esac; last=\"$a\"; done\n" +
		"emit() { i=0; while [ \"$i\" -lt \"$1\" ]; do printf '{\"title\":\"%s\",\"webpage_url\":\"https://example.test/%s/%s\"}\\n' \"$last\" \"$last\" \"$i\"; i=$((i+1)); done; }\n" +
		"fail() { echo \"$1\" >&2; exit 1; }\n" +
		"case \"$mode\" in\nsearch)\n" + search + "\n;;\ninspect)\n" + inspect + "\n;;\n*)\n" + other + "\n;;\nesac\n"
}

type searchAnswer struct {
	specGlob string
	body     string
}

func searchAnswers(fallback string, answers ...searchAnswer) string {
	script := "case \"$last\" in\n"
	for _, answer := range answers {
		script += answer.specGlob + ") " + answer.body + " ;;\n"
	}
	return script + "*) " + fallback + " ;;\nesac"
}

func installSearch(t *testing.T, fallback string, answers ...searchAnswer) string {
	t.Helper()
	return installYtDlp(t, ytDlpByMode(searchAnswers(fallback, answers...), "", ""))
}

func searchedSpecs(t *testing.T, calls string) []string {
	t.Helper()
	var specs []string
	for _, argv := range recordedCalls(t, calls) {
		if slices.Contains(argv, "--dump-json") {
			specs = append(specs, argv[len(argv)-1])
		}
	}
	slices.Sort(specs)
	return specs
}

func loggedErrors(t *testing.T, logs *logging.Capture, msg string) []string {
	t.Helper()
	var errs []string
	for line := range strings.Lines(logs.String()) {
		var record struct {
			Msg   string `json:"msg"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if record.Msg == msg {
			errs = append(errs, record.Error)
		}
	}
	return errs
}

func isSourceUnavailable(err error) bool {
	var unavailable *domain.SourceUnavailableError
	return errors.As(err, &unavailable)
}

func downloadWithScript(t *testing.T, body string) (ports.FetchedAudio, error) {
	t.Helper()
	outDir := t.TempDir()
	installYtDlp(t, strings.ReplaceAll(body, "OUT", outDir))
	return ytdlp.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep).Download(context.Background(), youTubeWatchURL, outDir)
}

func stubDownload(outDir string) string {
	file := filepath.Join(outDir, "stub.m4a")
	return "head -c 20480 /dev/zero > '" + file + "'\necho 'altune-output-path:" + file + "'"
}

const liveJarContents = "# live cookie jar"

type cookieCapture struct {
	calls       string
	pathFile    string
	contentFile string
}

func captureCookieJar(t *testing.T, output string) cookieCapture {
	t.Helper()
	dir := t.TempDir()
	capture := cookieCapture{pathFile: filepath.Join(dir, "cookie-path"), contentFile: filepath.Join(dir, "cookie-content")}
	capture.calls = installYtDlp(t, "prev=\"\"\n"+
		"for arg in \"$@\"; do\n"+
		"  if [ \"$prev\" = \"--cookies\" ]; then\n"+
		"    printf '%s' \"$arg\" > '"+capture.pathFile+"'\n"+
		"    cp \"$arg\" '"+capture.contentFile+"'\n"+
		"  fi\n"+
		"  prev=\"$arg\"\n"+
		"done\n"+
		output)
	return capture
}

func (c cookieCapture) assertTempCopyUsedAndRemoved(t *testing.T, liveJar string) {
	t.Helper()
	usedPath, err := os.ReadFile(c.pathFile)
	if err != nil {
		t.Fatalf("yt-dlp never received a --cookies flag: %v", err)
	}
	if string(usedPath) == liveJar {
		t.Fatalf("yt-dlp used the live cookie file %q, want a temp copy", liveJar)
	}
	content, err := os.ReadFile(c.contentFile)
	if err != nil || string(content) != liveJarContents {
		t.Fatalf("temp cookie copy content = %q, err %v, want the live jar's contents", content, err)
	}
	if _, err := os.Stat(string(usedPath)); !os.IsNotExist(err) {
		t.Fatalf("temp cookie copy %q still exists after the call returned, want it removed", usedPath)
	}
}
