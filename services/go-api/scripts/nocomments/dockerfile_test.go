package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func dockerfileSpanLines(t *testing.T, src string) []int {
	t.Helper()
	spans, err := dockerfileComments([]byte(src))
	if err != nil {
		t.Fatalf("dockerfileComments: %v", err)
	}
	var lines []int
	for _, s := range spans {
		lines = append(lines, s.line)
	}
	return lines
}

func TestDockerfileStripKeepsCodeAndDirectives(t *testing.T) {
	src := "# syntax=docker/dockerfile:1\n# header\n# more\nFROM a\n  # indented\nRUN echo a#b\nRUN x \\\n  # inside\n  y\nRUN <<EOF\n# kept\nEOF\n"
	out, n, err := strip([]byte(src), dockerfileKind)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	want := "# syntax=docker/dockerfile:1\nFROM a\nRUN echo a#b\nRUN x \\\n  y\nRUN <<EOF\n# kept\nEOF\n"
	if string(out) != want || n != 4 {
		t.Fatalf("strip = %d comments\n%q\nwant %q", n, out, want)
	}
}

func TestDockerfileCommentsFindsOnlyThePlainCommentAfterTrickyLines(t *testing.T) {
	cases := map[string]string{
		"quoted heredoc marker": "FROM a\nRUN echo \"see <<EOF for docs\"\n# c\n",
		"quote across continue": "FROM a\nRUN echo \"a \\\" continued by ` continues <<REAL here\"\n# c\n",
		"arithmetic shift":      "FROM a\nRUN x=$((1<<2))\n# c\n",
		"lone angle":            "FROM a\nRUN echo <\n# c\n",
		"dashed delimiter":      "FROM a\nRUN <<MY-EOF\n# b\nMY-EOF\n# c\n",
		"two heredocs":          "FROM a\nRUN <<A cat - <<-B\n# a\nA\n\t# b\n\tB\n# c\n",
		"crlf heredoc":          "FROM a\r\nRUN <<EOF\r\n# b\r\nEOF\r\n# c\r\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			lines := dockerfileSpanLines(t, src)
			if len(lines) != 1 {
				t.Fatalf("spans on lines %v, want exactly one", lines)
			}
			if got := strings.Split(strings.ReplaceAll(src, "\r", ""), "\n")[lines[0]-1]; got != "# c" {
				t.Fatalf("span covers %q, want \"# c\"", got)
			}
		})
	}
}

func TestDockerfileCommentsDirectiveWindowEndsAtFirstNonDirective(t *testing.T) {
	cases := map[string]struct {
		src  string
		want []int
	}{
		"after comment": {"# plain\n# syntax=x\nFROM a\n", []int{1, 2}},
		"after blank":   {"\n# syntax=x\nFROM a\n", []int{2}},
		"at top":        {"# syntax=x\nFROM a\n", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := dockerfileSpanLines(t, c.src)
			if len(got) != len(c.want) {
				t.Fatalf("lines = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("lines = %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestDockerfileSameRejectsChangesBeyondComments(t *testing.T) {
	base := "# escape=`\nFROM a\nRUN <<EOF\necho hi\nEOF\n"
	rejected := map[string]string{
		"heredoc body": "# escape=`\nFROM a\nRUN <<EOF\nrm -rf x\nEOF\n",
		"dropped step": "# escape=`\nFROM a\n",
		"escape token": "FROM a\nRUN <<EOF\necho hi\nEOF\n",
	}
	for name, after := range rejected {
		t.Run(name, func(t *testing.T) {
			if err := dockerfileKind.same([]byte(base), []byte(after)); err == nil {
				t.Fatal("same returned nil, want an error")
			}
		})
	}
	stripped := "# escape=`\nFROM a\nRUN <<EOF\necho hi\nEOF\n"
	withComment := "# escape=`\n# c\nFROM a\nRUN <<EOF\necho hi\nEOF\n"
	if err := dockerfileKind.same([]byte(withComment), []byte(stripped)); err != nil {
		t.Fatalf("same on a correct strip: %v", err)
	}
}

func TestDockerfileMatch(t *testing.T) {
	for path, want := range map[string]bool{
		"Dockerfile":          true,
		"a/b/Dockerfile.prod": true,
		"x/app.dockerfile":    true,
		"Dockerfile-old":      false,
		"main.go":             false,
	} {
		if got := dockerfileKind.match(path, nil); got != want {
			t.Errorf("match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestDockerfileCommentsRepoWideTrackedDockerfiles(t *testing.T) {
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
		if !dockerfileKind.match(relPath, nil) {
			continue
		}
		t.Run(relPath, func(t *testing.T) {
			src, err := os.ReadFile(root + "/" + relPath)
			if err != nil {
				t.Fatalf("read %s: %v", relPath, err)
			}
			stripped, _, err := strip(src, dockerfileKind)
			if err != nil {
				t.Fatalf("strip %s: %v", relPath, err)
			}
			if err := dockerfileKind.same(src, stripped); err != nil {
				t.Fatalf("%s: %v", relPath, err)
			}
			remaining, err := dockerfileComments(stripped)
			if err != nil {
				t.Fatalf("reparse %s: %v", relPath, err)
			}
			if len(remaining) != 0 {
				t.Fatalf("%s: %d comments remain after strip", relPath, len(remaining))
			}
		})
	}
}
