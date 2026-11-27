package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const usage = "usage: go run ./scripts/lintsleeps [--summary] <dir>...\n  lists every time.Sleep call in the go files under each dir, and in its _test.go files every clock.Sleep, time.AfterFunc, time.NewTimer and time.Tick call and every <-time.After receive (also as _ = or x :=), except a timeout case in a select whose body fails the test through t, b or tb or returns, which is a bound, that is not inside a synctest.Test or synctest.Run callback, and exits 1 when there are any; --summary prints only the count per directory"

const clockImportPath = "altune/go-api/internal/shared/clock"

var skippedDirs = map[string]bool{"vendor": true, "testdata": true, "node_modules": true}

type violation struct {
	path string
	line int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdout, stderr io.Writer) int {
	summary := false
	var dirs []string
	for _, arg := range args {
		switch {
		case arg == "--summary":
			summary = true
		case strings.HasPrefix(arg, "-"):
			say(stderr, "%s\n", usage)
			return 2
		default:
			dirs = append(dirs, arg)
		}
	}
	if len(dirs) == 0 {
		say(stderr, "%s\n", usage)
		return 2
	}
	var found []violation
	for _, dir := range dirs {
		hits, err := scanDir(dir)
		if err != nil {
			say(stderr, "lintsleeps: %v\n", err)
			return 2
		}
		found = append(found, hits...)
	}
	if summary {
		printSummary(stdout, found)
	} else {
		for _, v := range found {
			say(stdout, "%s:%d: wait outside a synctest bubble (sleep or timer); wait on the event itself, a condition with a deadline or a channel the test controls, or run the test inside synctest.Test\n", v.path, v.line)
		}
	}
	say(stdout, "sleeps and timer waits outside a synctest bubble: %d\n", len(found))
	if len(found) > 0 {
		return 1
	}
	return 0
}

func printSummary(w io.Writer, found []violation) {
	perDir := map[string]int{}
	for _, v := range found {
		perDir[filepath.Dir(v.path)]++
	}
	dirs := make([]string, 0, len(perDir))
	for d := range perDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		say(w, "%6d  %s\n", perDir[d], d)
	}
}

func scanDir(root string) ([]violation, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var found []violation
	err = filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != resolved && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		hits, err := scanFile(path)
		if err != nil {
			return err
		}
		found = append(found, hits...)
		return nil
	})
	return found, err
}

func importName(file *ast.File, importPath, fallback string) string {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) != importPath {
			continue
		}
		switch {
		case imp.Name == nil:
			return fallback
		case imp.Name.Name == "_" || imp.Name.Name == ".":
			return ""
		default:
			return imp.Name.Name
		}
	}
	return ""
}

func scanFile(path string) ([]violation, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	timePkg := importName(file, "time", "time")
	clockPkg := ""
	if strings.HasSuffix(path, "_test.go") {
		clockPkg = importName(file, clockImportPath, "clock")
	}
	if timePkg == "" && clockPkg == "" {
		return nil, nil
	}
	synctestPkg := importName(file, "testing/synctest", "synctest")
	isTest := strings.HasSuffix(path, "_test.go")
	var found []violation
	inSelect := map[ast.Stmt]bool{}
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		ast.Inspect(n, func(m ast.Node) bool {
			if timePkg != "" && isSelector(m, timePkg, "Sleep") {
				found = append(found, violation{path, fset.Position(m.Pos()).Line})
				return true
			}
			if clause, isClause := m.(*ast.CommClause); isClause && clause.Comm != nil {
				inSelect[clause.Comm] = true
				if isTest && timePkg != "" && receivesFromAfter(clause.Comm, timePkg) && !endsTheTest(clause.Body) {
					found = append(found, violation{path, fset.Position(clause.Pos()).Line})
				}
			}
			if stmt, isStmt := m.(ast.Stmt); isStmt && !inSelect[stmt] && isTest && timePkg != "" && receivesFromAfter(stmt, timePkg) {
				found = append(found, violation{path, fset.Position(m.Pos()).Line})
				return true
			}
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			if clockPkg != "" && isSelector(call.Fun, clockPkg, "Sleep") {
				found = append(found, violation{path, fset.Position(m.Pos()).Line})
				return true
			}
			if isTest && timePkg != "" && isTimedTrigger(call.Fun, timePkg) {
				found = append(found, violation{path, fset.Position(m.Pos()).Line})
				return true
			}
			if synctestPkg != "" && (isSelector(call.Fun, synctestPkg, "Test") || isSelector(call.Fun, synctestPkg, "Run")) {
				for _, arg := range call.Args {
					if _, isLit := arg.(*ast.FuncLit); !isLit {
						walk(arg)
					}
				}
				return false
			}
			return true
		})
	}
	walk(file)
	return found, nil
}

func endsTheTest(body []ast.Stmt) bool {
	ends := false
	for _, stmt := range body {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.ReturnStmt:
				ends = true
			case *ast.CallExpr:
				ends = ends || failsTheTest(x.Fun)
			}
			return !ends
		})
	}
	return ends
}

func failsTheTest(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		recv, isIdent := f.X.(*ast.Ident)
		if !isIdent || (recv.Name != "t" && recv.Name != "b" && recv.Name != "tb") {
			return false
		}
		name := f.Sel.Name
		return strings.HasPrefix(name, "Fatal") || strings.HasPrefix(name, "Error") || strings.HasPrefix(name, "Skip") || name == "FailNow" || name == "Fail"
	case *ast.Ident:
		return f.Name == "panic"
	}
	return false
}

func receivesFromAfter(stmt ast.Stmt, timePkg string) bool {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		return isReceiveFromAfter(s.X, timePkg)
	case *ast.AssignStmt:
		return len(s.Rhs) == 1 && isReceiveFromAfter(s.Rhs[0], timePkg)
	}
	return false
}

func isReceiveFromAfter(e ast.Expr, timePkg string) bool {
	recv, ok := e.(*ast.UnaryExpr)
	if !ok || recv.Op != token.ARROW {
		return false
	}
	call, ok := recv.X.(*ast.CallExpr)
	return ok && isSelector(call.Fun, timePkg, "After")
}

func isTimedTrigger(fun ast.Node, timePkg string) bool {
	return isSelector(fun, timePkg, "AfterFunc") || isSelector(fun, timePkg, "NewTimer") || isSelector(fun, timePkg, "Tick")
}

func isSelector(e ast.Node, pkg, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == name
}
