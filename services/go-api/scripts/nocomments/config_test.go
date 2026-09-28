package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGitPatternMatchByBasename(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".gitignore", true},
		{"apps/mobile/.gitignore", true},
		{".ignore", true},
		{".gitattributes", true},
		{".dockerignore", true},
		{".github/CODEOWNERS", false},
		{"apps/mobile/.npmrc", false},
		{".gitmessage", false},
		{"services/go-api/.env.example", false},
		{".claude/slos.tsv", false},
		{"README.md", false},
	}
	for _, tc := range cases {
		if got := gitPatternKind.match(tc.path, nil); got != tc.want {
			t.Errorf("gitPatternKind.match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestLineConfigMatchByBasenameAndPattern(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".github/CODEOWNERS", true},
		{".gitmessage", true},
		{"services/go-api/.env.example", true},
		{"services/go-api/deploy/.env.staging.example", true},
		{".claude/slos.tsv", true},
		{".gitignore", false},
		{".gitattributes", false},
		{"apps/mobile/.npmrc", false},
		{"README.md", false},
		{"services/go-api/.air.toml", false},
		{".envrc", false},
	}
	for _, tc := range cases {
		if got := lineConfigKind.match(tc.path, nil); got != tc.want {
			t.Errorf("lineConfigKind.match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestNpmrcMatchByBasename(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".npmrc", true},
		{"apps/mobile/.npmrc", true},
		{".gitignore", false},
		{"services/go-api/.env.example", false},
	}
	for _, tc := range cases {
		if got := npmrcKind.match(tc.path, nil); got != tc.want {
			t.Errorf("npmrcKind.match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

const gitignoreFixture = "# OS files\n.DS_Store\n\\#literal\nnode_modules/\n; not a comment marker in gitignore\ndist/\n"

func TestGitPatternCommentsStripsColumnZeroHashCommentsAndKeepsEscapedHashAndSemicolon(t *testing.T) {
	spans, err := gitPatternComments([]byte(gitignoreFixture))
	if err != nil {
		t.Fatalf("gitPatternComments: %v", err)
	}
	stripped := string(deleteSpans([]byte(gitignoreFixture), spans))

	for _, want := range []string{"\\#literal", "node_modules/", "dist/", "; not a comment marker in gitignore"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped output missing %q\n%s", want, stripped)
		}
	}
	for _, dontWant := range []string{"OS files"} {
		if strings.Contains(stripped, dontWant) {
			t.Errorf("stripped output still contains %q\n%s", dontWant, stripped)
		}
	}
	if err := gitPatternSame([]byte(gitignoreFixture), []byte(stripped)); err != nil {
		t.Fatalf("gitPatternSame: %v", err)
	}
}

func TestGitPatternKeepsAnIndentedHashAsAPatternNotAComment(t *testing.T) {
	got := stripFileThroughRun(t, ".gitattributes", "  # spaces\n\t#tab\n*.sh text\n# top-level\n")
	want := "  # spaces\n\t#tab\n*.sh text\n"
	if got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

const envExampleFixture = "# top comment\nKEY=a#b\nOTHER=value\n# another line comment\nMORE=x\n"

func TestLineConfigCommentsKeepsAMidLineHashInAnEnvValue(t *testing.T) {
	spans, err := lineConfigComments([]byte(envExampleFixture))
	if err != nil {
		t.Fatalf("lineConfigComments: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	stripped := string(deleteSpans([]byte(envExampleFixture), spans))
	if !strings.Contains(stripped, "KEY=a#b") {
		t.Errorf("stripped output lost the mid-line hash in KEY=a#b\n%s", stripped)
	}
	if strings.Contains(stripped, "top comment") || strings.Contains(stripped, "another line comment") {
		t.Errorf("stripped output still has a comment line\n%s", stripped)
	}
}

const tsvFixture = "col_a\tcol_b\n# header note\nval1\t#tag\nval2\tval3\n"

func TestLineConfigCommentsKeepsAHashInsideATSVCell(t *testing.T) {
	spans, err := lineConfigComments([]byte(tsvFixture))
	if err != nil {
		t.Fatalf("lineConfigComments: %v", err)
	}
	stripped := string(deleteSpans([]byte(tsvFixture), spans))
	if !strings.Contains(stripped, "val1\t#tag") {
		t.Errorf("stripped output lost the TSV cell holding a hash\n%s", stripped)
	}
	if strings.Contains(stripped, "header note") {
		t.Errorf("stripped output still has the comment line\n%s", stripped)
	}
}

const npmrcFixture = "; leading semicolon comment\nlegacy-peer-deps=true\n# leading hash comment\nregistry=https://example.com\n"

func TestNpmrcCommentsStripsSemicolonLines(t *testing.T) {
	spans, err := npmrcComments([]byte(npmrcFixture))
	if err != nil {
		t.Fatalf("npmrcComments: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	stripped := string(deleteSpans([]byte(npmrcFixture), spans))
	for _, want := range []string{"legacy-peer-deps=true", "registry=https://example.com"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("stripped output missing %q\n%s", want, stripped)
		}
	}
	for _, dontWant := range []string{"leading semicolon comment", "leading hash comment"} {
		if strings.Contains(stripped, dontWant) {
			t.Errorf("stripped output still contains %q\n%s", dontWant, stripped)
		}
	}
	if err := npmrcSame([]byte(npmrcFixture), []byte(stripped)); err != nil {
		t.Fatalf("npmrcSame: %v", err)
	}
}

func TestLineConfigStripKeepsSemicolonLinesOutsideNpmrc(t *testing.T) {
	for name, src := range map[string]string{
		".gitignore":   ";semi\n",
		".env.example": ";X=1\n",
		"rows.tsv":     ";cell\tv\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := stripFileThroughRun(t, name, src); got != src {
				t.Fatalf("stripped = %q, want unchanged %q", got, src)
			}
		})
	}
}

func TestLineConfigSameRejectsAKeptLineThatChanged(t *testing.T) {
	before := []byte("# c\nKEEP\n")
	after := []byte("CHANGED\n")
	if err := lineConfigSame(before, after); err == nil {
		t.Fatal("lineConfigSame: want error when a kept line changed, got nil")
	}
}

func TestLineConfigSameRejectsExtraLinesInTheStrippedOutput(t *testing.T) {
	before := []byte("# c\nKEEP\n")
	after := []byte("KEEP\nEXTRA\n")
	if err := lineConfigSame(before, after); err == nil {
		t.Fatal("lineConfigSame: want error when stripped output has extra lines, got nil")
	}
}

func repoWideCheck(t *testing.T, k kind) {
	t.Helper()
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
		if !k.match(relPath, nil) {
			continue
		}
		t.Run(relPath, func(t *testing.T) {
			src, err := os.ReadFile(root + "/" + relPath)
			if err != nil {
				t.Fatalf("read %s: %v", relPath, err)
			}
			spans, err := k.comments(src)
			if err != nil {
				t.Fatalf("comments %s: %v", relPath, err)
			}
			stripped := deleteSpans(src, spans)
			if err := k.same(src, stripped); err != nil {
				t.Fatalf("%s: %v", relPath, err)
			}
			remaining, err := k.comments(stripped)
			if err != nil {
				t.Fatalf("reparse stripped %s: %v", relPath, err)
			}
			if len(remaining) != 0 {
				t.Fatalf("%s: %d comments remain after strip", relPath, len(remaining))
			}
		})
	}
}

func TestGitPatternCommentsRepoWideTrackedFiles(t *testing.T) {
	repoWideCheck(t, gitPatternKind)
}

func TestLineConfigCommentsRepoWideTrackedFiles(t *testing.T) {
	repoWideCheck(t, lineConfigKind)
}

func TestNpmrcCommentsRepoWideTrackedFiles(t *testing.T) {
	repoWideCheck(t, npmrcKind)
}

func TestRunCheckReportsLineConfigViolations(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "# a comment\nnode_modules/\n")

	var stdout strings.Builder
	code := run([]string{"check", dir}, &stdout)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), ".gitignore:1") {
		t.Fatalf("output missing .gitignore:1:\n%s", stdout.String())
	}
}

func TestLineConfigStripKeepsABackslashEscapedHashAtLineStartAndMidLine(t *testing.T) {
	src := "\\#lit\na\\#b\n#c\n"
	if got := stripFileThroughRun(t, ".gitignore", src); got != "\\#lit\na\\#b\n" {
		t.Fatalf("stripped = %q, want %q", got, "\\#lit\na\\#b\n")
	}
}

func TestLineConfigStripRemovesHashLinesIndentedBySpacesOrTabs(t *testing.T) {
	got := stripFileThroughRun(t, "CODEOWNERS", "  # spaces\n\t#tab\n* @a\n")
	want := "* @a\n"
	if got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestLineConfigStripKeepsASemicolonLaterInAnNpmrcLineAndRemovesAnIndentedOne(t *testing.T) {
	got := stripFileThroughRun(t, ".npmrc", "  ; c\nk=v ; not a comment\n")
	if got != "k=v ; not a comment\n" {
		t.Fatalf("stripped = %q, want %q", got, "k=v ; not a comment\n")
	}
}

func TestLineConfigStripKeepsAHashAfterCodeownersOwners(t *testing.T) {
	src := "* @a #team\n"
	if got := stripFileThroughRun(t, "CODEOWNERS", src); got != src {
		t.Fatalf("stripped = %q, want unchanged %q", got, src)
	}
}

func TestLineConfigStripOfAnEmptyFileLeavesItEmptyAndCheckReportsZero(t *testing.T) {
	if got := stripFileThroughRun(t, ".gitignore", ""); got != "" {
		t.Fatalf("stripped = %q, want empty", got)
	}
	dir := t.TempDir()
	writeFile(t, dir, ".env.example", "")
	var stdout strings.Builder
	if code := run([]string{"check", dir + "/.env.example"}, &stdout); code != 0 {
		t.Fatalf("check exit = %d, want 0; output:\n%s", code, stdout.String())
	}
}

func TestLineConfigStripRemovesAFinalCommentLineWithoutNewline(t *testing.T) {
	if got := stripFileThroughRun(t, ".gitignore", "keep\n#only"); got != "keep\n" {
		t.Fatalf("stripped = %q, want %q", got, "keep\n")
	}
}

func TestLineConfigStripKeepsCRLFOnTheLinesItKeeps(t *testing.T) {
	if got := stripFileThroughRun(t, ".gitignore", "#c\r\nkeep\r\nmore\r\n"); got != "keep\r\nmore\r\n" {
		t.Fatalf("stripped = %q, want %q", got, "keep\r\nmore\r\n")
	}
}
