package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const shellFixture = `#!/usr/bin/env bash
# header comment
# second header line

func_name() {
  local x=1 # shellcheck disable=SC2034
  echo "a#b"
  if true; then
    # indented comment
    echo hello # trailing note
  fi
}

echo '# not a comment'
echo "a # b"
cat <<HEREDOC
# kept
HEREDOC

arr=(1 2 3)
echo "${arr[@]}"
echo "${#arr[@]}"
echo "$#"
f=/path/to/file
echo "${f##*/}"
a#b
`

func TestShellCommentsFixtureKeepsCodeAndDirectives(t *testing.T) {
	spans, err := shellComments([]byte(shellFixture))
	if err != nil {
		t.Fatalf("shellComments: %v", err)
	}
	if len(spans) != 5 {
		t.Fatalf("spans = %d, want 5", len(spans))
	}

	stripped := string(deleteSpans([]byte(shellFixture), spans))

	for _, want := range []string{
		"#!/usr/bin/env bash",
		`echo '# not a comment'`,
		`echo "a # b"`,
		"# kept",
		`echo "${#arr[@]}"`,
		`echo "$#"`,
		`echo "${f##*/}"`,
		"a#b",
	} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped output missing %q\n%s", want, stripped)
		}
	}

	for _, dontWant := range []string{
		"header comment",
		"second header line",
		"shellcheck disable=SC2034",
		"indented comment",
		"trailing note",
	} {
		if strings.Contains(stripped, dontWant) {
			t.Errorf("stripped output still contains %q\n%s", dontWant, stripped)
		}
	}

	if err := shellCommentFreePrintsEqual([]byte(shellFixture), []byte(stripped)); err != nil {
		t.Fatalf("shellCommentFreePrintsEqual: %v", err)
	}

	for _, line := range strings.Split(stripped, "\n") {
		if trimmed := strings.TrimRight(line, " \t"); trimmed != line {
			t.Errorf("stripped output leaves trailing whitespace on %q", line)
		}
	}

	remaining, err := shellComments([]byte(stripped))
	if err != nil {
		t.Fatalf("shellComments on stripped: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("stripped output still has %d comments", len(remaining))
	}
}

func TestShellCommentsRepoWideTrackedScripts(t *testing.T) {
	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("git rev-parse --show-toplevel unavailable")
	}
	root := strings.TrimSpace(string(rootOut))

	lsOut, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	for _, relPath := range strings.Split(strings.TrimSpace(string(lsOut)), "\n") {
		relPath := relPath
		full := root + "/" + relPath
		head := shellFileHead(t, full)
		if !shellKind.match(relPath, head) {
			continue
		}
		t.Run(relPath, func(t *testing.T) {
			src, err := os.ReadFile(full)
			if err != nil {
				t.Fatalf("read %s: %v", relPath, err)
			}
			spans, err := shellComments(src)
			if err != nil {
				t.Fatalf("shellComments %s: %v", relPath, err)
			}
			stripped := deleteSpans(src, spans)
			if err := shellCommentFreePrintsEqual(src, stripped); err != nil {
				t.Fatalf("%s: %v", relPath, err)
			}
			remaining, err := shellComments(stripped)
			if err != nil {
				t.Fatalf("reparse stripped %s: %v", relPath, err)
			}
			if len(remaining) != 0 {
				t.Fatalf("%s: %d comments remain after strip", relPath, len(remaining))
			}
		})
	}
}

func TestShellCommentsFailsLoudlyWhenTheWalkMissesAComment(t *testing.T) {
	_, err := shellComments([]byte("time # c1\n:\n"))
	if err == nil {
		t.Fatal("shellComments: want error for a comment the syntax walk misses, got nil")
	}

	stdout := &strings.Builder{}
	code := run([]string{"check", writeTempShellFile(t, "time # c1\n:\n")}, stdout)
	if code != 2 {
		t.Fatalf("check exit = %d, want 2", code)
	}

	out := strings.Builder{}
	stripCode := run([]string{"strip", writeTempShellFile(t, "time # c1\n:\n")}, &out)
	if stripCode != 1 {
		t.Fatalf("strip exit = %d, want 1", stripCode)
	}
}

func TestShellCommentsFailsLoudlyWhenAHerestringPrecedesAWalkMissedComment(t *testing.T) {
	_, err := shellComments([]byte("cat <<< \"hi\"\ntime # x\n:\n"))
	if err == nil {
		t.Fatal("shellComments: want error for a comment the syntax walk misses after a herestring, got nil")
	}

	stdout := &strings.Builder{}
	code := run([]string{"check", writeTempShellFile(t, "cat <<< \"hi\"\ntime # x\n:\n")}, stdout)
	if code != 2 {
		t.Fatalf("check exit = %d, want 2", code)
	}
}

func TestResidualScanReportsTheLineOfAWalkMissedCommentAcrossNewlines(t *testing.T) {
	_, err := shellComments([]byte("echo a\necho b\ntime # x\n:\n"))
	if err == nil {
		t.Fatal("shellComments: want error for a comment the syntax walk misses, got nil")
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("error = %v, want it to name line 3", err)
	}
}

func TestResidualScanAdvancesPastAnUnparseableHeredocOperatorAndStillFindsTheNextComment(t *testing.T) {
	line, found := residualCommentLine([]byte("cat <<\n# x\n"))
	if !found || line != 2 {
		t.Fatalf("residualCommentLine = (%d, %v), want (2, true)", line, found)
	}
}

func TestSkipAnsiCQuoteCountsLinesAcrossBackslashAndPlainNewlines(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantI    int
		wantLine int
	}{
		{"plain embedded newline", "a\nb'rest", 4, 2},
		{"escaped newline continuation", "a\\\nb'rest", 5, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, line := skipAnsiCQuote([]byte(tc.src), 0, 1)
			if i != tc.wantI || line != tc.wantLine {
				t.Fatalf("skipAnsiCQuote(%q) = (%d, %d), want (%d, %d)", tc.src, i, line, tc.wantI, tc.wantLine)
			}
		})
	}
}

func TestSkipDoubleQuoteCountsLinesAcrossABackslashNewlineContinuation(t *testing.T) {
	i, line := skipDoubleQuote([]byte("a\\\nb\"rest"), 0, 1)
	if i != 5 || line != 2 {
		t.Fatalf("skipDoubleQuote = (%d, %d), want (5, 2)", i, line)
	}
}

func TestBraceExpansionStepAdvancesPositionLineAndDepth(t *testing.T) {
	cases := []struct {
		name                       string
		src                        string
		i, line, depth             int
		wantI, wantLine, wantDepth int
	}{
		{"nested open brace increases depth", "{x}", 0, 1, 1, 1, 1, 2},
		{"escaped char advances by two", "\\nrest", 0, 1, 1, 2, 1, 1},
		{"embedded newline advances the line", "\nrest", 0, 1, 1, 1, 2, 1},
		{"ansi-c quote inside a brace expansion", "$'a'rest", 1, 1, 1, 4, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, line, depth := braceExpansionStep([]byte(tc.src), tc.i, tc.line, tc.depth)
			if i != tc.wantI || line != tc.wantLine || depth != tc.wantDepth {
				t.Fatalf("braceExpansionStep(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tc.src, i, line, depth, tc.wantI, tc.wantLine, tc.wantDepth)
			}
		})
	}
}

func writeTempShellFile(t *testing.T, contents string) string {
	t.Helper()
	path := t.TempDir() + "/script.sh"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp shell file: %v", err)
	}
	return path
}

func shellFileHead(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return buf[:n]
}
