package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const workflowFixture = `# top of file
# second top line
name: ci # trailing
on:
  push:
    branches: [main] # flow tail
# between steps
jobs:
  build:
    steps:
      # indented note
      - run: |
          # note
          echo "${#arr[@]}" # tail
          echo ${{ fromJSON('{"a":{"b":1}}') }}
          echo ${{ x == '{' }} # tail2
          echo a#b http://x/#f
      - run: | # explain
          # note two
          echo two
      - run: &a |
          # note three
          echo three
      - "run": |
          # note four
          echo four
      - name: 'it''s
          # x'
        with:
          keep1: '# quoted'
          keep2: "a # b"
          keep3: a#b
          keep4: http://x/#f
          script: |
            # kept in non-run block
            text
          foo: | # c
            body
      - run: |
          # last node
          echo last
`

func TestWorkflowStripKeepsNonComments(t *testing.T) {
	out, n, err := strip([]byte(workflowFixture), workflowKind)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	got := string(out)
	gone := []string{"top of file", "second top", "# trailing", "flow tail", "between steps", "indented note", "# note", "explain", "note two", "note three", "note four", "last node", "# c\n"}
	for _, g := range gone {
		if strings.Contains(got, g) {
			t.Errorf("stripped output still contains %q:\n%s", g, got)
		}
	}
	kept := []string{"'# quoted'", `"a # b"`, "a#b", "http://x/#f", "# kept in non-run block", "${#arr[@]}", `fromJSON('{"a":{"b":1}}')`, "x == '{' }} # tail2", "'it''s\n          # x'", "echo \"${#arr[@]}\" # tail\n", "foo: |\n"}
	for _, k := range kept {
		if !strings.Contains(got, k) {
			t.Errorf("stripped output lost %q:\n%s", k, got)
		}
	}
	if n != 13 {
		t.Errorf("stripped %d comments, want 13", n)
	}
}

func TestWorkflowLeftoversAreReportedButNeverEdited(t *testing.T) {
	src := []byte("steps:\n  - run: |\n      echo a # tail\n      echo b\n")
	lines, err := workflowKind.leftovers(src)
	if err != nil {
		t.Fatalf("leftovers: %v", err)
	}
	if len(lines) != 1 || lines[0] != 3 {
		t.Fatalf("leftovers = %v, want [3]", lines)
	}
	out, n, err := strip(src, workflowKind)
	if err != nil || n != 0 || string(out) != string(src) {
		t.Fatalf("strip = %q, %d, %v; want untouched source", out, n, err)
	}
}

func TestPlainYamlTreatsRunAsPlainYaml(t *testing.T) {
	src := "# c\nsteps:\n  - run: |\n      echo \"unbalanced\n"
	out, n, err := strip([]byte(src), yamlKind)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if n != 1 || string(out) != "steps:\n  - run: |\n      echo \"unbalanced\n" {
		t.Fatalf("strip = %q, %d", out, n)
	}
}

func TestYamlKindsMatchDisjointPaths(t *testing.T) {
	cases := []struct {
		path            string
		workflow, plain bool
	}{
		{".github/workflows/ci.yml", true, false},
		{"../../.gitea/actions/x/action.yaml", true, false},
		{"services/go-api/deploy/compose.prod.yml", false, true},
		{".golangci.yml", false, true},
		{"notes.txt", false, false},
	}
	for _, tc := range cases {
		if got := workflowKind.match(tc.path, nil); got != tc.workflow {
			t.Errorf("workflowKind.match(%q) = %v", tc.path, got)
		}
		if got := yamlKind.match(tc.path, nil); got != tc.plain {
			t.Errorf("yamlKind.match(%q) = %v", tc.path, got)
		}
	}
}

func TestYamlRefusesUnsafeShapesLoudly(t *testing.T) {
	cases := []struct {
		name string
		k    kind
		src  string
		want string
	}{
		{"lone CR", yamlKind, "a: 1\rb: 2\n", "lone CR"},
		{"folded run", workflowKind, "steps:\n  - run: >\n      echo a\n", "folded run"},
		{"explicit indent", yamlKind, "a: |2\n   x\n", "explicit block indentation"},
		{"bad shell", workflowKind, "steps:\n  - run: |\n      echo \"open\n", "run: block at line 2"},
	}
	for _, tc := range cases {
		_, err := tc.k.comments([]byte(tc.src))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestYamlSameRejectsAliasRetargetAndSurvivingComment(t *testing.T) {
	base := "a: &a {x: 1}\nb: &b {x: 2}\nc:\n  <<: *a\n"
	if err := yamlSame([]byte(base), []byte(strings.Replace(base, "*a", "*b", 1))); err == nil {
		t.Error("retargeted alias accepted")
	}
	err := yamlSame([]byte("a: 1 # c\n"), []byte("a: 1 # c\n"))
	if err == nil || !strings.Contains(err.Error(), "comment survived strip at line 1") {
		t.Errorf("surviving comment err = %v", err)
	}
}

func TestYamlStripHandlesCRLF(t *testing.T) {
	out, n, err := strip([]byte("# c\r\na: 1 # t\r\n"), yamlKind)
	if err != nil || n != 2 || string(out) != "a: 1\r\n" {
		t.Fatalf("strip = %q, %d, %v", out, n, err)
	}
}

func TestTrackedYamlFilesStripToEquivalentTrees(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Skipf("no git root: %v", err)
	}
	listed, err := exec.Command("git", "-C", root, "ls-files", "*.yml", "*.yaml").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	for _, rel := range strings.Fields(string(listed)) {
		full := filepath.Join(root, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		k, ok, err := matchKind(full)
		if err != nil || !ok {
			t.Fatalf("%s: no kind (%v)", rel, err)
		}
		out, _, err := strip(src, k)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			continue
		}
		if err := assertNoSpans(out, k); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

func assertNoSpans(out []byte, k kind) error {
	spans, err := k.comments(out)
	if err != nil {
		return err
	}
	if len(spans) != 0 {
		return fmt.Errorf("comment left after strip at line %d", spans[0].line)
	}
	return nil
}
