package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureSource = `//go:build integration

package fixture

import "embed"

// Foo doc comment.
func Foo() string {
	s := "// not a comment"
	raw := ` + "`/* x */`" + `
	_ = raw
	return s // trailing comment
}

/* block comment above Bar */
func Bar() {}

// clip embeds a short audio sample.
// It is loaded once at init.
//go:embed clip.mp3
var Clip embed.FS

func Baz() error {
	err := doWork() //nolint:nilerr
	return err
}

func doWork() error { return nil }
`

func TestStripFixtureKeepsCodeAndDirectives(t *testing.T) {
	out, n, err := strip([]byte(fixtureSource), "fixture.go")
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if n != 5 {
		t.Fatalf("removed = %d, want 5", n)
	}

	result := string(out)

	for _, want := range []string{
		`s := "// not a comment"`,
		"`/* x */`",
		"//go:build integration",
		"//go:embed clip.mp3",
		"//nolint:nilerr",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q\n%s", want, result)
		}
	}

	for _, dontWant := range []string{
		"Foo doc comment",
		"trailing comment",
		"block comment above Bar",
		"clip embeds a short audio sample",
		"It is loaded once at init",
	} {
		if strings.Contains(result, dontWant) {
			t.Errorf("result still contains %q\n%s", dontWant, result)
		}
	}

	if _, err := parser.ParseFile(token.NewFileSet(), "fixture.go", out, parser.ParseComments); err != nil {
		t.Fatalf("stripped output does not parse: %v", err)
	}
}

func TestStripPreservesModuleASTs(t *testing.T) {
	for _, root := range []string{"../..", "../../../overseer"} {
		root := root
		t.Run(root, func(t *testing.T) {
			files, err := collectGoFiles(root)
			if err != nil {
				t.Fatalf("collectGoFiles(%s): %v", root, err)
			}
			if len(files) == 0 {
				t.Fatalf("no .go files found under %s", root)
			}
			for _, file := range files {
				file := file
				t.Run(file, func(t *testing.T) {
					src, err := os.ReadFile(file)
					if err != nil {
						t.Fatalf("read %s: %v", file, err)
					}

					out, _, err := strip(src, file)
					if err != nil {
						t.Fatalf("strip %s: %v", file, err)
					}

					assertOnlyKeptComments(t, file, out)
					assertSameNodeSequence(t, file, src, out)
				})
			}
		})
	}
}

func assertOnlyKeptComments(t *testing.T, file string, out []byte) {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, out, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("stripped output for %s does not parse: %v", file, err)
	}
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			if !isKept(comment.Text) {
				t.Fatalf("%s: stripped output still has a dropped comment: %q", file, comment.Text)
			}
		}
	}
}

func assertSameNodeSequence(t *testing.T, file string, src, out []byte) {
	t.Helper()
	fset := token.NewFileSet()
	before, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse original %s: %v", file, err)
	}
	after, err := parser.ParseFile(token.NewFileSet(), file, out, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse stripped %s: %v", file, err)
	}

	beforeSig := nodeSignature(before)
	afterSig := nodeSignature(after)
	if len(beforeSig) != len(afterSig) {
		t.Fatalf("%s: node count changed: before %d, after %d", file, len(beforeSig), len(afterSig))
	}
	for i := range beforeSig {
		if beforeSig[i] != afterSig[i] {
			t.Fatalf("%s: node %d differs: before %q, after %q", file, i, beforeSig[i], afterSig[i])
		}
	}
}

func nodeSignature(node ast.Node) []string {
	var sig []string
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch v := n.(type) {
		case *ast.Ident:
			sig = append(sig, fmt.Sprintf("Ident:%s", v.Name))
		case *ast.BasicLit:
			sig = append(sig, fmt.Sprintf("BasicLit:%s:%s", v.Kind, v.Value))
		case *ast.BinaryExpr:
			sig = append(sig, fmt.Sprintf("BinaryExpr:%s", v.Op))
		case *ast.UnaryExpr:
			sig = append(sig, fmt.Sprintf("UnaryExpr:%s", v.Op))
		case *ast.IncDecStmt:
			sig = append(sig, fmt.Sprintf("IncDecStmt:%s", v.Tok))
		case *ast.AssignStmt:
			sig = append(sig, fmt.Sprintf("AssignStmt:%s", v.Tok))
		default:
			sig = append(sig, fmt.Sprintf("%T", n))
		}
		return true
	})
	return sig
}

func TestRunStripsSamplePackageAndKeepsItBuildable(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "go.mod", "module sample\n\ngo 1.26.6\n")
	writeFile(t, dir, "clip.txt", "hello\n")
	writeFile(t, dir, "pkg.go", `package sample

// Greeting is the default greeting.
func Greeting() string {
	return "hello" // trailing note
}
`)
	writeFile(t, dir, "embedded.go", `package sample

import _ "embed"

// Clip holds the embedded sample.
//go:embed clip.txt
var Clip string
`)
	writeFile(t, dir, "build_unix.go", `//go:build !windows

package sample

func Only() int {
	return 1
}
`)
	writeFile(t, dir, "pkg_test.go", `package sample

import "testing"

func TestClipNotEmpty(t *testing.T) {
	if len(Clip) == 0 {
		t.Fatal("clip is empty")
	}
}
`)

	var stdout bytes.Buffer
	code := run([]string{dir}, &stdout)
	if code != 0 {
		t.Fatalf("run exit = %d, output: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "stripped ") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}

	stripped, err := os.ReadFile(filepath.Join(dir, "pkg.go"))
	if err != nil {
		t.Fatalf("read stripped pkg.go: %v", err)
	}
	if strings.Contains(string(stripped), "Greeting is the default greeting") {
		t.Fatalf("pkg.go still has its doc comment:\n%s", stripped)
	}

	runGo(t, dir, "build", "./...")
	runGo(t, dir, "vet", "./...")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runGo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestRunWithNoPathsReturnsUsageExitCode(t *testing.T) {
	var stdout bytes.Buffer
	code := run(nil, &stdout)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunSkipsAnUnparsableFileAndStillProcessesTheRest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "broken.go", "package broken\n\nfunc(\n")
	writeFile(t, dir, "ok.go", "package broken\n\n// drop me\nfunc OK() {}\n")

	var stdout bytes.Buffer
	code := run([]string{dir}, &stdout)
	if code != 1 {
		t.Fatalf("exit = %d, want 1, output: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "broken.go") {
		t.Fatalf("output does not name the failing file: %s", stdout.String())
	}

	okContent, err := os.ReadFile(filepath.Join(dir, "ok.go"))
	if err != nil {
		t.Fatalf("read ok.go: %v", err)
	}
	if strings.Contains(string(okContent), "drop me") {
		t.Fatalf("ok.go was not stripped despite broken.go failing:\n%s", okContent)
	}

	brokenContent, err := os.ReadFile(filepath.Join(dir, "broken.go"))
	if err != nil {
		t.Fatalf("read broken.go: %v", err)
	}
	if string(brokenContent) != "package broken\n\nfunc(\n" {
		t.Fatalf("broken.go was rewritten despite failing to parse:\n%s", brokenContent)
	}
}

func TestStripIsIdempotent(t *testing.T) {
	first, n, err := strip([]byte(fixtureSource), "fixture.go")
	if err != nil {
		t.Fatalf("first strip: %v", err)
	}
	if n == 0 {
		t.Fatalf("first strip removed nothing")
	}

	second, n2, err := strip(first, "fixture.go")
	if err != nil {
		t.Fatalf("second strip: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second strip removed %d comments, want 0", n2)
	}
	if string(second) != string(first) {
		t.Fatalf("second strip changed already-stripped source")
	}
}

func TestStripKeepsTheSyntaxTreeWhenCommentsSitInsideExpressions(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		removed int
	}{
		{
			name:    "block comments around a binary operator",
			src:     "package p\n\nvar a, b = 1, 2\nvar c = a /* x */ + b\nvar d = a/* x */-b\nvar e = a /* x */ - /* y */ -b\n",
			removed: 4,
		},
		{
			name:    "comments inside a case list",
			src:     "package p\n\nfunc f(x int) int {\n\tswitch x {\n\tcase 1, // one\n\t\t2: // two\n\t\treturn 1\n\t// dangling\n\tcase 3 /* three */, 4:\n\t\treturn 2\n\t}\n\treturn 0\n}\n",
			removed: 4,
		},
		{
			name:    "comments after trailing commas in a composite literal",
			src:     "package p\n\nvar r = []string{\n\t\"a\", /* x\n\t*/\n\t\"b\", // y\n}\n\nvar m = map[string]int{\"k\": 1 /* z */}\n",
			removed: 3,
		},
		{
			name:    "comments between call arguments",
			src:     "package p\n\nfunc g(a, b int) {}\n\nfunc f() {\n\tg(1, // first\n\t\t2) // second\n}\n",
			removed: 2,
		},
		{
			name:    "comments around struct tags",
			src:     "package p\n\ntype T struct {\n\tA int `json:\"a\"` // trailing\n\tB int /* before */ `json:\"b\"`\n\t// above\n\tC int `json:\"c // not\"`\n}\n",
			removed: 3,
		},
		{
			name:    "comments after return and a continued condition",
			src:     "package p\n\nfunc f(a int) bool {\n\tif a == 0 {\n\t\treturn /* x */ true // y\n\t}\n\treturn a == 1 || // z\n\t\ta == 2\n}\n",
			removed: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, n, err := strip([]byte(tc.src), "expr.go")
			if err != nil {
				t.Fatalf("strip: %v", err)
			}
			if n != tc.removed {
				t.Fatalf("removed = %d, want %d\n%s", n, tc.removed, out)
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), "expr.go", out, parser.ParseComments)
			if err != nil {
				t.Fatalf("stripped output does not parse: %v\n%s", err, out)
			}
			if len(parsed.Comments) != 0 {
				t.Fatalf("stripped output still has %d comment groups:\n%s", len(parsed.Comments), out)
			}
			assertSameNodeSequence(t, "expr.go", []byte(tc.src), out)
		})
	}
}

func TestStripRemovesAPackageDocComment(t *testing.T) {
	out, n, err := strip([]byte("// Package p does things.\npackage p\n"), "doc.go")
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if n != 1 {
		t.Fatalf("removed = %d, want 1", n)
	}
	if string(out) != "package p\n" {
		t.Fatalf("got %q, want %q", out, "package p\n")
	}
}

func TestStripKeepsEveryGoColonDirectiveButNotASpacedLookalike(t *testing.T) {
	src := "package p\n\n//go:generate stringer -type=Kind\n// go:generate not a directive\n//go:noinline\nfunc f() {}\n"
	out, n, err := strip([]byte(src), "gen.go")
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if n != 1 {
		t.Fatalf("removed = %d, want 1\n%s", n, out)
	}
	want := "package p\n\n//go:generate stringer -type=Kind\n//go:noinline\nfunc f() {}\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestRunKeepsABuildConstraintGluedToAPackageDocAndTheFileStaysExcluded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module sample\n\ngo 1.26.6\n")
	writeFile(t, dir, "main.go", "package main\n\n// main runs.\nfunc main() {}\n")
	writeFile(t, dir, "tool.go", "//go:build ignore\n// Tool is a standalone generator.\npackage main\n\nfunc main() {}\n")

	var stdout bytes.Buffer
	if code := run([]string{dir}, &stdout); code != 0 {
		t.Fatalf("run exit = %d, output: %s", code, stdout.String())
	}

	tool, err := os.ReadFile(filepath.Join(dir, "tool.go"))
	if err != nil {
		t.Fatalf("read tool.go: %v", err)
	}
	if !strings.HasPrefix(string(tool), "//go:build ignore\n") {
		t.Fatalf("tool.go lost its build constraint:\n%s", tool)
	}
	if strings.Contains(string(tool), "standalone generator") {
		t.Fatalf("tool.go kept its package doc:\n%s", tool)
	}

	runGo(t, dir, "build", "./...")
	runGo(t, dir, "vet", "./...")
}

func TestRunStripsTestFilesSkipsVendorTestdataNodeModulesAndLeavesCommentFreeFilesByteForByte(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"vendor/v", "testdata", "node_modules/n", "sub"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	skipped := "package x\n\n// keep me\nfunc X() {}\n"
	for _, sub := range []string{"vendor/v", "testdata", "node_modules/n"} {
		writeFile(t, dir, filepath.Join(sub, "x.go"), skipped)
	}
	writeFile(t, dir, "sub/a.go", "package sub\n\n// a\nfunc A() {}\n")
	writeFile(t, dir, "sub/a_test.go", "package sub\n\nimport \"testing\"\n\n// t\nfunc TestA(t *testing.T) {}\n")
	crlf := "package sub\r\n\r\nfunc C() int {\r\n\treturn 1\r\n}\r\n"
	writeFile(t, dir, "sub/crlf.go", crlf)
	unformatted := "package sub\nfunc U()  int {\n  return 1\n}\n"
	writeFile(t, dir, "sub/unformatted.go", unformatted)

	var stdout bytes.Buffer
	if code := run([]string{dir}, &stdout); code != 0 {
		t.Fatalf("run exit = %d, output: %s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "stripped 2 comments in 2 files") {
		t.Fatalf("summary = %q, want it to report 2 comments in 2 files", stdout.String())
	}

	for _, sub := range []string{"vendor/v", "testdata", "node_modules/n"} {
		got, err := os.ReadFile(filepath.Join(dir, sub, "x.go"))
		if err != nil {
			t.Fatalf("read %s: %v", sub, err)
		}
		if string(got) != skipped {
			t.Fatalf("%s/x.go was rewritten:\n%s", sub, got)
		}
	}
	testFile, err := os.ReadFile(filepath.Join(dir, "sub/a_test.go"))
	if err != nil {
		t.Fatalf("read a_test.go: %v", err)
	}
	if strings.Contains(string(testFile), "// t") {
		t.Fatalf("a_test.go was not stripped:\n%s", testFile)
	}
	for name, want := range map[string]string{"sub/crlf.go": crlf, "sub/unformatted.go": unformatted} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s has no comments but was rewritten: %q", name, got)
		}
	}
}
