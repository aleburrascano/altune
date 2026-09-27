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
