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
	"strconv"
	"strings"
)

const usage = "usage: go run ./scripts/lintdoors [--summary] [<go-api dir>]\n  every module (internal/<module>, or internal/<group>/<module> for a grouping directory) has one door, its root package, and may have a second, <module>test, that only _test.go files import; lists each outside import of any other package of a module and each non-test import of a test-support package, and exits 1 when there are any; --summary prints, per module, its door size, how much of it is used outside, and its open packages with their importers"

var groups = map[string]bool{"platform": true, "shared": true}

const (
	kindSideDoor = iota
	kindTestSupport
)

type violation struct {
	file     string
	line     int
	pkg      string
	module   string
	sub      string
	importer string
	kind     int
}

const (
	usedByProduction = 1
	usedByTests      = 2
)

type scan struct {
	modulePath string
	root       string
	violations []violation
	modules    map[string]bool
	pkgNames   map[string]string
	used       map[string]map[string]int
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdout, stderr io.Writer) int {
	summary := false
	root := "."
	for _, arg := range args {
		switch {
		case arg == "--summary":
			summary = true
		case strings.HasPrefix(arg, "-"):
			say(stderr, "%s\n", usage)
			return 2
		default:
			root = arg
		}
	}
	s, err := scanTree(root)
	if err != nil {
		say(stderr, "lintdoors: %v\n", err)
		return 2
	}
	if summary {
		if err := printSummary(stdout, s); err != nil {
			say(stderr, "lintdoors: %v\n", err)
			return 2
		}
	} else {
		for _, v := range s.violations {
			if v.kind == kindTestSupport {
				say(stdout, "%s:%d: non-test code imports the test-support package %s; only _test.go files may\n", v.file, v.line, v.pkg)
				continue
			}
			say(stdout, "%s:%d: imports %s, a non-root package of module %s; import %s/internal/%s and move what is needed behind its root\n", v.file, v.line, v.pkg, v.module, s.modulePath, v.module)
		}
	}
	say(stdout, "imports through a module's side door or of test support from production: %d\n", len(s.violations))
	if len(s.violations) > 0 {
		return 1
	}
	return 0
}

func modulePathOf(root string) (string, error) {
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(goMod), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no module line in %s", filepath.Join(root, "go.mod"))
}

func splitModule(rest string) (module, sub string) {
	parts := strings.Split(rest, "/")
	n := 1
	if groups[parts[0]] {
		n = 2
	}
	if len(parts) < n {
		return "", ""
	}
	return strings.Join(parts[:n], "/"), strings.Join(parts[n:], "/")
}

func lastSegment(p string) string {
	return p[strings.LastIndex(p, "/")+1:]
}

func isTestSupport(module, sub string) bool {
	if sub == "" {
		name := lastSegment(module)
		return strings.HasPrefix(name, "test")
	}
	return sub == lastSegment(module)+"test"
}

func discoverModules(root string) (map[string]bool, error) {
	modules := map[string]bool{}
	top, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		return nil, err
	}
	for _, e := range top {
		if !e.IsDir() {
			continue
		}
		if !groups[e.Name()] {
			modules[e.Name()] = true
			continue
		}
		members, err := os.ReadDir(filepath.Join(root, "internal", e.Name()))
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if m.IsDir() {
				modules[e.Name()+"/"+m.Name()] = true
			}
		}
	}
	return modules, nil
}

func packageName(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err == nil {
			return f.Name.Name
		}
	}
	return ""
}

func scanTree(root string) (*scan, error) {
	modulePath, err := modulePathOf(root)
	if err != nil {
		return nil, err
	}
	modules, err := discoverModules(root)
	if err != nil {
		return nil, err
	}
	s := &scan{modulePath: modulePath, root: root, modules: modules, pkgNames: map[string]string{}, used: map[string]map[string]int{}}
	for m := range modules {
		s.pkgNames[m] = packageName(filepath.Join(root, "internal", filepath.FromSlash(m)))
		s.used[m] = map[string]int{}
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		return s.scanFile(p)
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(s.violations, func(i, j int) bool {
		a, b := s.violations[i], s.violations[j]
		if a.file != b.file {
			return a.file < b.file
		}
		return a.line < b.line
	})
	return s, nil
}

func (s *scan) scanFile(file string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	rel, err := filepath.Rel(s.root, filepath.Dir(file))
	if err != nil {
		return err
	}
	importer := filepath.ToSlash(rel)
	ownModule := ""
	if rest, ok := strings.CutPrefix(importer, "internal/"); ok {
		ownModule, _ = splitModule(rest)
	}
	isTest := strings.HasSuffix(file, "_test.go")
	prefix := s.modulePath + "/internal/"
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		rest, ok := strings.CutPrefix(target, prefix)
		if !ok {
			continue
		}
		module, sub := splitModule(rest)
		if !s.modules[module] {
			continue
		}
		v := violation{file: file, line: fset.Position(imp.Pos()).Line, pkg: target, module: module, sub: sub, importer: importer}
		switch {
		case isTestSupport(module, sub) && !isTest:
			v.kind = kindTestSupport
			s.violations = append(s.violations, v)
		case sub == "" && module != ownModule:
			s.recordUse(f, imp, module, isTest)
		case sub != "" && module != ownModule && !isTestSupport(module, sub):
			v.kind = kindSideDoor
			s.violations = append(s.violations, v)
		}
	}
	return nil
}

func (s *scan) recordUse(f *ast.File, imp *ast.ImportSpec, module string, isTest bool) {
	alias := s.pkgNames[module]
	if imp.Name != nil {
		alias = imp.Name.Name
	}
	if alias == "" || alias == "_" || alias == "." {
		return
	}
	flag := usedByProduction
	if isTest {
		flag = usedByTests
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !sel.Sel.IsExported() {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias {
			s.used[module][sel.Sel.Name] |= flag
		}
		return true
	})
}

type door struct {
	exported    int
	prod, tests int
}

func (s *scan) doorOf(module string) (door, error) {
	names, err := rootExports(filepath.Join(s.root, "internal", filepath.FromSlash(module)))
	if err != nil {
		return door{}, err
	}
	d := door{exported: len(names)}
	for name := range names {
		switch s.used[module][name] {
		case usedByProduction, usedByProduction | usedByTests:
			d.prod++
		case usedByTests:
			d.tests++
		}
	}
	return d, nil
}

func printSummary(w io.Writer, s *scan) error {
	modules := make([]string, 0, len(s.modules))
	for m := range s.modules {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, m := range modules {
		d, err := s.doorOf(m)
		if err != nil {
			return err
		}
		open := map[string]map[string]int{}
		testSupport := 0
		for _, v := range s.violations {
			if v.module != m {
				continue
			}
			if v.kind == kindTestSupport {
				testSupport++
				continue
			}
			if open[v.sub] == nil {
				open[v.sub] = map[string]int{}
			}
			open[v.sub][v.importer]++
		}
		say(w, "%s: %d exported at the root, %d used outside by production code, %d used outside only by tests, %d unused outside; %d open packages, %d production imports of test support\n",
			m, d.exported, d.prod, d.tests, d.exported-d.prod-d.tests, len(open), testSupport)
		subs := make([]string, 0, len(open))
		for p := range open {
			subs = append(subs, p)
		}
		sort.Strings(subs)
		for _, p := range subs {
			importers := make([]string, 0, len(open[p]))
			for i := range open[p] {
				importers = append(importers, i)
			}
			sort.Strings(importers)
			say(w, "  %s\n", m+"/"+p)
			for _, i := range importers {
				say(w, "    %3d  %s\n", open[p][i], i)
			}
		}
	}
	return nil
}

func rootExports(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, name), err)
		}
		collectExported(f, names)
	}
	return names, nil
}

func collectExported(f *ast.File, names map[string]bool) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.IsExported() {
				names[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					if sp.Name.IsExported() {
						names[sp.Name.Name] = true
					}
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						if n.IsExported() {
							names[n.Name] = true
						}
					}
				}
			}
		}
	}
}
