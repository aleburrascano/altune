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
