package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestTomlMatchByExtension(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"services/go-api/.air.toml", true},
		{"config.toml", true},
		{"config.TOML", false},
		{"config.yaml", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		if got := tomlKind.match(tc.path, nil); got != tc.want {
			t.Errorf("tomlKind.match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

const tomlFixture = `# top-level whole-line comment
title = "example" # trailing comment after a value

[build]
cmd = "go build" # another trailing comment
basic = "a # b"
literal = 'lit # c'
multi_basic = """
first line
a # inside a multi-line basic string
last line"""
multi_literal = '''
one # inside a multi-line literal string
two'''

[build.inline]
point = { x = 1, y = 2 } # trailing comment on an inline table

[build.matrix]
rows = [
  1, 2, 3,
  # comment inside an array
  4, 5,
]
`

func TestTomlCommentsFixtureStripsWholeLineAndTrailingCommentsAndKeepsHashesInStrings(t *testing.T) {
	spans, err := tomlComments([]byte(tomlFixture))
	if err != nil {
		t.Fatalf("tomlComments: %v", err)
	}
	stripped := string(deleteSpans([]byte(tomlFixture), spans))

	for _, want := range []string{
		`basic = "a # b"`,
		`literal = 'lit # c'`,
		"a # inside a multi-line basic string",
		"one # inside a multi-line literal string",
	} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped output lost %q\n%s", want, stripped)
		}
	}
	for _, dontWant := range []string{
		"top-level whole-line comment",
		"trailing comment after a value",
		"another trailing comment",
		"trailing comment on an inline table",
		"comment inside an array",
	} {
		if strings.Contains(stripped, dontWant) {
			t.Errorf("stripped output still contains %q\n%s", dontWant, stripped)
		}
	}

	if err := tomlDecodesEqual([]byte(tomlFixture), []byte(stripped)); err != nil {
		t.Fatalf("tomlDecodesEqual: %v", err)
	}

	remaining, err := tomlComments([]byte(stripped))
	if err != nil {
		t.Fatalf("tomlComments on stripped: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("stripped output still has %d comments", len(remaining))
	}
}

func TestTomlCommentsFixtureDecodesToTheSameValueTreeAsGoTomlDirectly(t *testing.T) {
	spans, err := tomlComments([]byte(tomlFixture))
	if err != nil {
		t.Fatalf("tomlComments: %v", err)
	}
	stripped := deleteSpans([]byte(tomlFixture), spans)

	var want, got map[string]any
	if err := toml.Unmarshal([]byte(tomlFixture), &want); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if err := toml.Unmarshal(stripped, &got); err != nil {
		t.Fatalf("decode stripped: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("stripped decodes to %#v, want %#v", got, want)
	}
}

func TestTomlDecodesEqualRejectsAChangedValueTree(t *testing.T) {
	before := []byte("k = 1\n")
	after := []byte("k = 2\n")
	if err := tomlDecodesEqual(before, after); err == nil {
		t.Fatal("tomlDecodesEqual: want error for a changed value tree, got nil")
	}
}

func TestAirTomlHasNoComments(t *testing.T) {
	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("git rev-parse --show-toplevel unavailable")
	}
	root := strings.TrimSpace(string(rootOut))
	src, err := os.ReadFile(root + "/services/go-api/.air.toml")
	if err != nil {
		t.Skip(".air.toml not present")
	}
	spans, err := tomlComments(src)
	if err != nil {
		t.Fatalf("tomlComments .air.toml: %v", err)
	}
	if len(spans) != 0 {
		t.Fatalf(".air.toml has %d comments, want 0", len(spans))
	}
}

func TestTomlCommentsRepoWideTrackedFiles(t *testing.T) {
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
		if !tomlKind.match(relPath, nil) {
			continue
		}
		t.Run(relPath, func(t *testing.T) {
			src, err := os.ReadFile(root + "/" + relPath)
			if err != nil {
				t.Fatalf("read %s: %v", relPath, err)
			}
			spans, err := tomlComments(src)
			if err != nil {
				t.Fatalf("tomlComments %s: %v", relPath, err)
			}
			stripped := deleteSpans(src, spans)
			if err := tomlDecodesEqual(src, stripped); err != nil {
				t.Fatalf("%s: %v", relPath, err)
			}
			remaining, err := tomlComments(stripped)
			if err != nil {
				t.Fatalf("reparse stripped %s: %v", relPath, err)
			}
			if len(remaining) != 0 {
				t.Fatalf("%s: %d comments remain after strip", relPath, len(remaining))
			}
		})
	}
}

func TestRunCheckReportsTomlViolations(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.toml", "# a comment\nk = 1\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "a.toml:1") {
		t.Fatalf("output missing a.toml:1:\n%s", stdout.String())
	}
}

func stripFileThroughRun(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, name, content)
	var stdout strings.Builder
	if code := run([]string{"strip", dir + "/" + name}, &stdout); code != 0 {
		t.Fatalf("strip exit = %d, want 0; output:\n%s", code, stdout.String())
	}
	got, err := os.ReadFile(dir + "/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(got)
}

func TestTomlStripKeepsLinesStartingWithHashInsideMultiLineStrings(t *testing.T) {
	cases := map[string]string{
		"basic":                               "k = \"\"\"\n# not a comment\n  # nor this\nx\"\"\"\n",
		"literal":                             "k = '''\n# not a comment\n  # nor this\n'''\n",
		"basic after a line-ending backslash": "k = \"\"\"\\\n  # kept\n  x\"\"\"\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if got := stripFileThroughRun(t, "a.toml", src); got != src {
				t.Fatalf("stripped = %q, want unchanged %q", got, src)
			}
		})
	}
}

func TestTomlStripKeepsHashAfterAnEscapedQuoteAndRemovesTheRealTrailingComment(t *testing.T) {
	got := stripFileThroughRun(t, "a.toml", "k = \"a \\\" # b\" # real\n")
	if want := "k = \"a \\\" # b\"\n"; got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestTomlStripKeepsHashInQuotedKeysAndTableHeaders(t *testing.T) {
	got := stripFileThroughRun(t, "a.toml", "\"#key\" = 1 # c\n[ \"#t\" ] # c\n'#lit' = 2\n")
	if want := "\"#key\" = 1\n[ \"#t\" ]\n'#lit' = 2\n"; got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestTomlStripRemovesCommentsAroundMultiLineArrayElementsHoldingHash(t *testing.T) {
	got := stripFileThroughRun(t, "a.toml", "arr = [ # c1\n  1, # c2\n  \"#x\", # c3\n  '#y',\n] # c4\n")
	if want := "arr = [\n  1,\n  \"#x\",\n  '#y',\n]\n"; got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestTomlStripRemovesATrailingCommentOnALastLineWithoutNewline(t *testing.T) {
	if got := stripFileThroughRun(t, "a.toml", "k = 1 # c"); got != "k = 1" {
		t.Fatalf("stripped = %q, want %q", got, "k = 1")
	}
}

func TestTomlStripRemovesACommentAfterAMultiLineStringClosedByFiveQuotes(t *testing.T) {
	cases := map[string][2]string{
		"basic":   {"k = \"\"\"a\"\"\"\"\"#x\n", "k = \"\"\"a\"\"\"\"\"\n"},
		"literal": {"k = '''a'''''#x\n", "k = '''a'''''\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := stripFileThroughRun(t, "a.toml", c[0]); got != c[1] {
				t.Fatalf("stripped = %q, want %q", got, c[1])
			}
		})
	}
}

func TestTomlStripKeepsCRLFOnWholeLineCommentRemoval(t *testing.T) {
	got := stripFileThroughRun(t, "a.toml", "a = 1\r\n# w\r\nj = \"x\"\r\n")
	if want := "a = 1\r\nj = \"x\"\r\n"; got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestTomlCheckFindsACommentAfterAMultiLineStringClosedByFourQuotes(t *testing.T) {
	for name, src := range map[string]string{
		"basic":   "k = \"\"\"a\"\"\"\"#\"\n",
		"literal": "k = '''a''''#'\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "a.toml", src)
			var stdout strings.Builder
			if code := run([]string{"check", dir + "/a.toml"}, &stdout); code != 1 {
				t.Fatalf("check exit = %d, want 1; output:\n%s", code, stdout.String())
			}
		})
	}
}

func TestTomlStripKeepsCRLFWhenRemovingATrailingComment(t *testing.T) {
	got := stripFileThroughRun(t, "a.toml", "k = 1 # c\r\nj = 2\r\n")
	if want := "k = 1\r\nj = 2\r\n"; got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}
