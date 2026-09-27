package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckExitsOneAndNamesFileLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hi.sh", "#!/bin/sh\n# hi\necho hi\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	wantLine := filepath.Join(dir, "hi.sh") + ":2"
	if !strings.Contains(stdout.String(), wantLine) {
		t.Fatalf("output missing %q:\n%s", wantLine, stdout.String())
	}
}

func TestRunCheckExitsZeroOnShebangOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "clean.sh", "#!/bin/sh\necho hi\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestRunDiffFlagsOnlyLinesAddedSinceBase(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")

	writeFile(t, dir, "a.sh", "#!/bin/sh\n# base comment\necho hi\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	writeFile(t, dir, "a.sh", "#!/bin/sh\n# base comment\necho hi\n# x\necho bye\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "second")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
	}()

	var stdout strings.Builder
	code := run([]string{"diff", base}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "a.sh:4") {
		t.Fatalf("output missing the added comment a.sh:4:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "a.sh:2") {
		t.Fatalf("output flagged the pre-existing base comment a.sh:2:\n%s", stdout.String())
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

func TestStripPreservesWhatTheScriptPrints(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"comment line after a line continuation", "echo a \\\n# x\necho c\n", "a\nc\n"},
		{"indented comment line after a line continuation", "echo a \\\n  # x\necho c\n", "a\nc\n"},
		{"comment line after a CRLF line continuation", "echo a \\\r\n# x\r\necho c\r\n", "a \r\nc\r\n"},
		{"trailing comment on a continued command", "printf '%s\\n' a \\\n  b # x\necho c\n", "a\nb\nc\n"},
		{"comment after a continuation at eof without newline", "echo a \\\n  # x", "a\n"},
		{"comments between case patterns", "set -- a\ncase $1 in\n  # c\n  a) echo A ;; # t\n  # d\n  *) echo B ;;\nesac\n", "A\n"},
		{"trailing comment inside command substitution", "x=$(echo hi # c\n)\necho \"$x\"\n", "hi\n"},
		{"comment line inside command substitution", "x=$(\n# c\necho hi\n)\necho \"$x\"\n", "hi\n"},
		{"comment inside backticks", "echo `echo hi # c`; echo after\n", "hi\nafter\n"},
		{"comment inside process substitution", "cat <(echo hi # c\n)\n", "hi\n"},
		{"trailing comment inside an array", "a=( x # c\n y )\necho \"${a[@]}\"\n", "x y\n"},
		{"comment line inside an array", "a=(\n # c\n x\n)\necho \"${#a[@]}\"\n", "1\n"},
		{"comment inside double brackets", "[[ -n x # c\n ]] && echo y\n", "y\n"},
		{"escaped hash pattern in double brackets", "x='#a'; [[ $x == \\#* ]] && echo m\n", "m\n"},
		{"tab-stripped heredoc with hash lines", "cat <<-EOF\n\t# body\n\tEOF\n", "# body\n"},
		{"quoted heredoc with a comment on its opener", "cat <<'EOF' # c\n# body $x\nEOF\necho z\n", "# body $x\nz\n"},
		{"double-quoted heredoc delimiter", "cat <<\"EOF\"\n# body\nEOF\n", "# body\n"},
		{"piped heredoc with a trailing comment", "cat <<EOF | tr a b # c\n# a\nEOF\n", "# b\n"},
		{"comment inside a substitution in a heredoc", "cat <<EOF\n$(echo hi # c\n)\nEOF\n", "hi\n"},
		{"hash after dollar brace equals and quote edges", "set -- 1 2; x=; echo $# ${#} ${x:-#} y=# '#'# \"$1\"#y \\# $'#' ${x/#a/b}\n", "2 2 # y=# ## 1#y # #\n"},
		{"comment right after a semicolon", "echo a;# x\necho b\n", "a\nb\n"},
		{"comment line at eof without newline", "echo hi\n# end", "hi\n"},
		{"trailing comment at eof without newline", "echo hi # end", "hi\n"},
		{"herestring is not misparsed as a heredoc", "cat <<< \"hi\" # c\necho after\n", "hi\nafter\n"},
		{"ansi-c quoted string with an escaped quote and a hash", "x=$'it\\'s a test # not a comment'\necho \"$x\" # c\n", "it's a test # not a comment\n"},
		{"heredoc delimiter after extra horizontal space", "cat <<   EOF\n# body\nEOF\n", "# body\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bashStdout(t, tc.src); got != tc.want {
				t.Fatalf("original script prints %q, want %q", got, tc.want)
			}
			dir := t.TempDir()
			writeFile(t, dir, "s.sh", tc.src)

			var stdout strings.Builder
			if code := run([]string{"strip", dir}, &stdout); code != 0 {
				t.Fatalf("strip(%q) exit = %d, want 0; output:\n%s", tc.src, code, stdout.String())
			}
			stripped, err := os.ReadFile(filepath.Join(dir, "s.sh"))
			if err != nil {
				t.Fatalf("read stripped: %v", err)
			}
			if got := bashStdout(t, string(stripped)); got != tc.want {
				t.Fatalf("strip(%q) = %q, which prints %q, want %q", tc.src, stripped, got, tc.want)
			}
			var check strings.Builder
			if code := run([]string{"check", dir}, &check); code != 0 {
				t.Fatalf("check after strip(%q) exit = %d, want 0; stripped %q; output:\n%s", tc.src, code, stripped, check.String())
			}
		})
	}
}

func TestCheckFlagsAShebangThatIsNotAtLineOneColumnOne(t *testing.T) {
	cases := []struct {
		name string
		src  string
		line string
	}{
		{"shebang on line two", "\n#!/bin/bash\necho hi\n", ":2"},
		{"indented shebang on line one", "  #!/bin/sh\necho hi\n", ":1"},
		{"second shebang under the first", "#!/bin/sh\n#!/bin/sh\necho hi\n", ":2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a.sh", tc.src)

			var stdout strings.Builder
			code := run([]string{"check", dir}, &stdout)

			if code != 1 {
				t.Fatalf("check(%q) exit = %d, want 1; output:\n%s", tc.src, code, stdout.String())
			}
			if want := filepath.Join(dir, "a.sh") + tc.line; !strings.Contains(stdout.String(), want) {
				t.Fatalf("check(%q) output missing %q:\n%s", tc.src, want, stdout.String())
			}
		})
	}
}

func TestStripKeepsCRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.sh", "echo hi # x\r\necho yo\r\n")

	var stdout strings.Builder
	code := run([]string{"strip", dir}, &stdout)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.sh"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "echo hi\r\necho yo\r\n"; string(got) != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestCheckExitsTwoOnAnUnreadableFileInsteadOfSkippingItAsClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.sh")
	writeFile(t, dir, "locked.sh", "# a real comment\necho hi\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 2 {
		t.Fatalf("exit = %d, want 2; output:\n%s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "comment violations: 0") {
		t.Fatalf("an unreadable file was reported as clean:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "locked.sh") {
		t.Fatalf("output does not name locked.sh:\n%s", stdout.String())
	}
}

func TestCheckExitsTwoOnAParseError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bad.sh", "f() { # x\n}\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 2 {
		t.Fatalf("exit = %d, want 2; output:\n%s", code, stdout.String())
	}
}

func TestStripLeavesAnUnparseableFileUntouchedAndStripsTheRest(t *testing.T) {
	dir := t.TempDir()
	bad := "f() { # x\n}\n"
	writeFile(t, dir, "bad.sh", bad)
	writeFile(t, dir, "good.sh", "echo hi # x\n")

	var stdout strings.Builder
	code := run([]string{"strip", dir}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "bad.sh") {
		t.Fatalf("output does not name bad.sh:\n%s", stdout.String())
	}
	gotBad, err := os.ReadFile(filepath.Join(dir, "bad.sh"))
	if err != nil {
		t.Fatalf("read bad: %v", err)
	}
	if string(gotBad) != bad {
		t.Fatalf("bad.sh = %q, want it untouched %q", gotBad, bad)
	}
	gotGood, err := os.ReadFile(filepath.Join(dir, "good.sh"))
	if err != nil {
		t.Fatalf("read good: %v", err)
	}
	if string(gotGood) != "echo hi\n" {
		t.Fatalf("good.sh = %q, want %q", gotGood, "echo hi\n")
	}
}

func TestSelectsExtensionlessShellByShebangAndLeavesOtherScriptsAlone(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tool", "#!/usr/bin/env bash\n# hi\necho hi\n")
	py := "#!/usr/bin/env python3\n# hi\nprint('hi')\n"
	writeFile(t, dir, "script", py)
	writeFile(t, dir, "run.py", py)
	writeFile(t, dir, "notes.txt", "# hi\n")

	var check strings.Builder
	code := run([]string{"check", dir}, &check)

	if code != 1 {
		t.Fatalf("check exit = %d, want 1; output:\n%s", code, check.String())
	}
	if want := filepath.Join(dir, "tool") + ":2"; !strings.Contains(check.String(), want) {
		t.Fatalf("check output missing %q:\n%s", want, check.String())
	}
	for _, name := range []string{"script:", "run.py", "notes.txt"} {
		if strings.Contains(check.String(), name) {
			t.Fatalf("check flagged non-shell file %s:\n%s", name, check.String())
		}
	}

	var strip strings.Builder
	if code := run([]string{"strip", dir}, &strip); code != 0 {
		t.Fatalf("strip exit = %d, want 0; output:\n%s", code, strip.String())
	}
	for _, name := range []string{"script", "run.py"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != py {
			t.Fatalf("strip changed non-shell file %s to %q", name, got)
		}
	}
}

func TestWalkSkipsNodeModulesVendorAndGit(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"node_modules", "vendor", ".git"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeFile(t, filepath.Join(dir, sub), "a.sh", "echo hi # x\n")
	}

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestStripReportsCountsAndASecondStripChangesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.sh", "#!/bin/sh\necho a # x\necho b # y\n")
	writeFile(t, dir, "b.sh", "echo c # z\n")
	writeFile(t, dir, "c.sh", "echo d\n")

	var first strings.Builder
	if code := run([]string{"strip", dir}, &first); code != 0 {
		t.Fatalf("first strip exit = %d, want 0; output:\n%s", code, first.String())
	}
	if !strings.Contains(first.String(), "stripped 3 comments in 2 files") {
		t.Fatalf("first strip output = %q, want it to report 3 comments in 2 files", first.String())
	}
	after, err := os.ReadFile(filepath.Join(dir, "a.sh"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != "#!/bin/sh\necho a\necho b\n" {
		t.Fatalf("a.sh = %q, want %q", after, "#!/bin/sh\necho a\necho b\n")
	}

	var second strings.Builder
	if code := run([]string{"strip", dir}, &second); code != 0 {
		t.Fatalf("second strip exit = %d, want 0; output:\n%s", code, second.String())
	}
	if !strings.Contains(second.String(), "stripped 0 comments in 0 files") {
		t.Fatalf("second strip output = %q, want 0 comments in 0 files", second.String())
	}
	again, err := os.ReadFile(filepath.Join(dir, "a.sh"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(again) != string(after) {
		t.Fatalf("second strip changed a.sh from %q to %q", after, again)
	}
}

func TestRunExitsTwoOnUsageErrors(t *testing.T) {
	cases := [][]string{
		nil,
		{"frobnicate"},
		{"check"},
		{"strip"},
		{"diff"},
	}
	for _, args := range cases {
		var stdout strings.Builder
		if code := run(args, &stdout); code != 2 {
			t.Errorf("run(%q) exit = %d, want 2; output:\n%s", args, code, stdout.String())
		}
	}
}

func TestRunDiffFromASubdirectoryReportsRepoRelativePaths(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")
	for _, sub := range []string{"svc", "deploy"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	writeFile(t, dir, "gone.sh", "echo gone\n")
	writeFile(t, dir, "svc/keep.txt", "x\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	gitCmd(t, dir, "rm", "-q", "gone.sh")
	writeFile(t, dir, "deploy/up.sh", "#!/bin/sh\necho up\n# x\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "second")
	chdirFor(t, filepath.Join(dir, "svc"))

	var stdout strings.Builder
	code := run([]string{"diff", base}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "deploy/up.sh:3") {
		t.Fatalf("output missing deploy/up.sh:3:\n%s", stdout.String())
	}
}

func chdirFor(t *testing.T, dir string) {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("chdir back: %v", err)
		}
	})
}

func bashStdout(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	cmd := exec.Command("bash", path)
	cmd.Dir = t.TempDir()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bash %q: %v", src, err)
	}
	return string(out)
}

func TestCheckFlagsACommentOnLineOneThatIsNotAShebang(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.sh", "# hi\necho hi\n")
	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if want := filepath.Join(dir, "a.sh") + ":1"; !strings.Contains(stdout.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, stdout.String())
	}
}

func TestStripTakesTheWholeLineOfAStandaloneComment(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.sh", "#!/bin/sh\necho a\n# c\n  # d\necho b\n")
	var stdout strings.Builder
	if code := run([]string{"strip", dir}, &stdout); code != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", code, stdout.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "a.sh"))
	if want := "#!/bin/sh\necho a\necho b\n"; string(got) != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestStripRemovesTrailingCommentEndingInBackslash(t *testing.T) {
	src := "echo a # x\\\necho b\n"
	dir := t.TempDir()
	writeFile(t, dir, "s.sh", src)
	var stdout strings.Builder
	if code := run([]string{"strip", dir}, &stdout); code != 0 {
		t.Fatalf("strip(%q) exit = %d, want 0; output:\n%s", src, code, stdout.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "s.sh"))
	if string(got) != "echo a\necho b\n" {
		t.Fatalf("strip(%q) = %q, want %q", src, got, "echo a\necho b\n")
	}
}

func TestRunDiffDoesNotFlagCommentsCarriedAcrossARename(t *testing.T) {
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "test")
	writeFile(t, dir, "old.sh", "#!/bin/sh\n# base comment\necho one\necho two\necho three\necho four\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	gitCmd(t, dir, "mv", "old.sh", "new.sh")
	writeFile(t, dir, "new.sh", "#!/bin/sh\n# base comment\necho one\necho two\necho three\necho four\n# x\n")
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "rename")
	chdirFor(t, dir)
	var stdout strings.Builder
	code := run([]string{"diff", base}, &stdout)
	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "new.sh:2") {
		t.Fatalf("output flagged the base comment carried by the rename, new.sh:2:\n%s", stdout.String())
	}
}
