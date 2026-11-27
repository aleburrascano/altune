package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func lint(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestSleepOutsideABubbleIsFlaggedWithFileAndLine(t *testing.T) {
	cases := map[string]struct{ name, body string }{
		"production file":        {"a.go", "package p\n\nimport \"time\"\n\nfunc f() { time.Sleep(time.Second) }\n"},
		"plain test":             {"a_test.go", "package p\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) { time.Sleep(time.Second) }\n"},
		"aliased time import":    {"a_test.go", "package p\n\nimport t \"time\"\n\nfunc f() { t.Sleep(1) }\n"},
		"next to a bubble":       {"a_test.go", "package p\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) {})\n\ttime.Sleep(1)\n}\n"},
		"sleep as a value":       {"a_test.go", "package p\n\nimport \"time\"\n\nfunc f() { g := time.Sleep; g(1) }\n"},
		"sleep in a helper":      {"a_test.go", "package p\n\nimport \"time\"\n\nfunc wait() { time.Sleep(1) }\n"},
		"named function as body": {"a_test.go", "package p\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc body(t *testing.T) { time.Sleep(1) }\n\nfunc TestX(t *testing.T) { synctest.Test(t, body) }\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.name, tc.body)

			code, out, _ := lint(t, dir)

			if code != 1 {
				t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
			}
			if !strings.Contains(out, filepath.Join(dir, tc.name)+":") {
				t.Fatalf("output does not name the file:\n%s", out)
			}
			if !strings.Contains(out, "outside a synctest bubble: 1") {
				t.Fatalf("output missing the count:\n%s", out)
			}
		})
	}
}

const clockImport = "\"altune/go-api/internal/shared/clock\""

func TestClockSleepCallInATestOutsideABubbleIsFlagged(t *testing.T) {
	cases := map[string]string{
		"plain call":       "package p\n\nimport (\n\t" + clockImport + "\n\t\"context\"\n)\n\nfunc f() { _ = clock.Sleep(context.Background(), 1) }\n",
		"aliased import":   "package p\n\nimport (\n\tc " + clockImport + "\n\t\"context\"\n)\n\nfunc f() { _ = c.Sleep(context.Background(), 1) }\n",
		"in a helper":      "package p\n\nimport (\n\t" + clockImport + "\n\t\"context\"\n)\n\nfunc wait() error { return clock.Sleep(context.Background(), 1) }\n",
		"next to a bubble": "package p\n\nimport (\n\t" + clockImport + "\n\t\"context\"\n\t\"testing\"\n\t\"testing/synctest\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) {})\n\t_ = clock.Sleep(context.Background(), 1)\n}\n",
		"in a method":      "package p\n\nimport (\n\t" + clockImport + "\n\t\"context\"\n)\n\ntype s struct{}\n\nfunc (s) Fetch(ctx context.Context) error { return clock.Sleep(ctx, 1) }\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a_test.go", body)

			code, out, _ := lint(t, dir)

			if code != 1 || !strings.Contains(out, "outside a synctest bubble: 1") {
				t.Fatalf("exit = %d, want 1 with a count of 1; output:\n%s", code, out)
			}
		})
	}
}

func TestTimerUsedAsAWaitInATestOutsideABubbleIsFlagged(t *testing.T) {
	cases := map[string]string{
		"delayed trigger":  "package p\n\nimport \"time\"\n\nfunc f(cancel func()) { time.AfterFunc(time.Second, cancel) }\n",
		"timer":            "package p\n\nimport \"time\"\n\nfunc f() { <-time.NewTimer(1).C }\n",
		"tick":             "package p\n\nimport \"time\"\n\nfunc f() { <-time.Tick(1) }\n",
		"bare receive":     "package p\n\nimport \"time\"\n\nfunc f() { <-time.After(1) }\n",
		"aliased time":     "package p\n\nimport t \"time\"\n\nfunc f(cancel func()) { t.AfterFunc(1, cancel) }\n",
		"next to a bubble": "package p\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) {})\n\t<-time.After(1)\n}\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a_test.go", body)

			code, out, _ := lint(t, dir)

			if code != 1 || !strings.Contains(out, "outside a synctest bubble: 1") {
				t.Fatalf("exit = %d, want 1 with a count of 1; output:\n%s", code, out)
			}
		})
	}
}

func TestTimerThatIsABoundOrAStimulusPasses(t *testing.T) {
	cases := map[string]struct{ name, body string }{
		"timeout in a select":       {"a_test.go", "package p\n\nimport \"time\"\n\nfunc f(ch chan int) {\n\tselect {\n\tcase <-ch:\n\tcase <-time.After(time.Second):\n\t}\n}\n"},
		"pacing ticker":             {"a_test.go", "package p\n\nimport \"time\"\n\nfunc f() { t := time.NewTicker(1); <-t.C }\n"},
		"delayed trigger in prod":   {"a.go", "package p\n\nimport \"time\"\n\nfunc f(cancel func()) { time.AfterFunc(time.Second, cancel) }\n"},
		"delayed trigger in bubble": {"a_test.go", "package p\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) { time.AfterFunc(1, func() {}) })\n}\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.name, tc.body)

			if code, out, _ := lint(t, dir); code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestClockSleepThatIsNotAFlaggedCallPasses(t *testing.T) {
	cases := map[string]struct{ name, body string }{
		"inside a bubble":        {"a_test.go", "package p\n\nimport (\n\t" + clockImport + "\n\t\"context\"\n\t\"testing\"\n\t\"testing/synctest\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) { _ = clock.Sleep(context.Background(), 1) })\n}\n"},
		"passed as a dependency": {"a_test.go", "package p\n\nimport " + clockImport + "\n\nfunc build(f func()) {}\n\nfunc g() { build(nil); _ = clock.Sleep }\n"},
		"in production code":     {"a.go", "package p\n\nimport (\n\t" + clockImport + "\n\t\"context\"\n)\n\nfunc backoff() error { return clock.Sleep(context.Background(), 1) }\n"},
		"another clock package":  {"a_test.go", "package p\n\nimport \"other/clock\"\n\nfunc f() { _ = clock.Sleep(1) }\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, tc.name, tc.body)

			if code, out, _ := lint(t, dir); code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestSleepInsideABubbleIsAllowed(t *testing.T) {
	cases := map[string]string{
		"synctest.Test":                  "package p\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) { time.Sleep(time.Second) })\n}\n",
		"synctest.Run":                   "package p\n\nimport (\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc f() { synctest.Run(func() { time.Sleep(time.Second) }) }\n",
		"nested goroutine in the bubble": "package p\n\nimport (\n\t\"testing\"\n\t\"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) {\n\tsynctest.Test(t, func(t *testing.T) {\n\t\tgo func() { time.Sleep(time.Second) }()\n\t\ttime.Sleep(2 * time.Second)\n\t})\n}\n",
		"aliased synctest import":        "package p\n\nimport (\n\t\"testing\"\n\tst \"testing/synctest\"\n\t\"time\"\n)\n\nfunc TestX(t *testing.T) { st.Test(t, func(t *testing.T) { time.Sleep(1) }) }\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a_test.go", body)

			code, out, _ := lint(t, dir)

			if code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestNotATimeSleepPasses(t *testing.T) {
	cases := map[string]string{
		"no time import":  "package p\n\nvar time = struct{ Sleep func(int) }{}\n\nfunc f() { time.Sleep(1) }\n",
		"another package": "package p\n\nimport \"x/clock\"\n\nfunc f() { clock.Sleep(1) }\n",
		"time.After":      "package p\n\nimport \"time\"\n\nfunc f() { <-time.After(1) }\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a.go", body)

			if code, out, _ := lint(t, dir); code != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", code, out)
			}
		})
	}
}

func TestTheClockPackageGetsNoException(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "internal/shared/clock/sleeper.go", "package clock\n\nimport \"time\"\n\nfunc Sleep(d time.Duration) { time.Sleep(d) }\n")

	code, out, _ := lint(t, dir)

	if code != 1 || !strings.Contains(out, "clock/sleeper.go") {
		t.Fatalf("exit = %d, want 1 naming the clock package; output:\n%s", code, out)
	}
}

func TestSkippedDirsAndSummary(t *testing.T) {
	dir := t.TempDir()
	body := "package p\n\nimport \"time\"\n\nfunc f() { time.Sleep(1); time.Sleep(2) }\n"
	writeFile(t, dir, "one/a.go", body)
	writeFile(t, dir, "two/a_test.go", "package p\n\nimport \"time\"\n\nfunc f() { time.Sleep(1) }\n")
	writeFile(t, dir, "vendor/x.go", body)
	writeFile(t, dir, "testdata/y.go", body)

	code, out, _ := lint(t, dir, "--summary")

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if !strings.Contains(out, "     2  "+filepath.Join(dir, "one")) || !strings.Contains(out, "     1  "+filepath.Join(dir, "two")) {
		t.Fatalf("summary does not count per directory:\n%s", out)
	}
	if strings.Contains(out, "vendor") || strings.Contains(out, "testdata") || strings.Contains(out, "a.go:") {
		t.Fatalf("summary lists skipped dirs or lines:\n%s", out)
	}
}

func TestUsageAndUnreadableInput(t *testing.T) {
	if code, _, stderr := lint(t); code != 2 || !strings.Contains(stderr, "usage:") {
		t.Fatalf("no arguments: exit = %d, stderr %q, want 2 and usage", code, stderr)
	}
	if code, _, _ := lint(t, "--bogus"); code != 2 {
		t.Fatalf("unknown flag: exit = %d, want 2", code)
	}
	if code, _, stderr := lint(t, filepath.Join(t.TempDir(), "missing")); code != 2 || !strings.Contains(stderr, "lintsleeps:") {
		t.Fatalf("missing dir: exit = %d, stderr %q, want 2 and a message", code, stderr)
	}
	dir := t.TempDir()
	writeFile(t, dir, "broken.go", "package p\n\nfunc (\n")
	if code, _, stderr := lint(t, dir); code != 2 || !strings.Contains(stderr, "parse") {
		t.Fatalf("unparsable file: exit = %d, stderr %q, want 2 and a parse message", code, stderr)
	}
}
