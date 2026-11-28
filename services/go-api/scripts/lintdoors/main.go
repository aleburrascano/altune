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

const usage = "usage: go run ./scripts/lintdoors [--summary] [--exports] [--shape] [<go-api dir>]\n  --shape checks the shape of every feature and platform module against the template (README.md of go-api, rules 1, 2, 5, 7, 10, 13, 18, 20 and 21) and prints each violation with the README section to read; every module (internal/<module>, or internal/<group>/<module> for a grouping directory) has one door, its root package, and may have a second, <module>test; test support is any package whose name ends in test, and only _test.go files or other test-support packages import it; lists each import of a test-support package from production code, and exits 1 when there are any; --exports also lists each root identifier that no file outside the module uses (a name used only by its own module's root-package tests is unexported, a name its own subpackages or test door use must stay and those tests go through the real door; used means pkg.Name through a plain or aliased import, a dot import is not followed); a flattened module is a feature module (internal/<module>) or a platform module (internal/platform/<module>), never one under shared, whose root holds exactly one non-test .go file, named after the module (library.go for library, outbound.go for platform/outbound): for it only a use by production code of another module counts, so a name used only by _test.go files, by test-support packages or by the module's own test door is listed too and moves to <module>test or is unexported; a module on the older layout, with more root files, keeps the broader rule above; --summary prints, per module, its door size and how much of it is used outside; side doors are checked by go-arch-lint (.go-arch-lint.doors.yml)"

const sharedGroup = "shared"

var groups = map[string]bool{"platform": true, sharedGroup: true}

const (
	kindTestSupport = iota
	kindUnusedExport
)

type violation struct {
	file     string
	line     int
	pkg      string
	module   string
	importer string
	kind     int
	col      int
	flat     bool
}

const (
	usedByProduction = 1
	usedByTests      = 2
	usedInside       = 4
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
	exports := false
	shape := false
	root := "."
	for _, arg := range args {
		switch {
		case arg == "--shape":
			shape = true
		case arg == "--summary":
			summary = true
		case arg == "--exports":
			exports = true
		case strings.HasPrefix(arg, "-"):
			say(stderr, "%s\n", usage)
			return 2
		default:
			root = arg
		}
	}
	if shape {
		return runShape(root, stdout, stderr)
	}
	s, err := scanTree(root)
	if err != nil {
		say(stderr, "lintdoors: %v\n", err)
		return 2
	}
	if exports {
		if err := s.addUnusedExports(); err != nil {
			say(stderr, "lintdoors: %v\n", err)
			return 2
		}
	}
	if summary {
		if err := printSummary(stdout, s); err != nil {
			say(stderr, "lintdoors: %v\n", err)
			return 2
		}
	} else {
		for _, v := range s.violations {
			if v.kind == kindUnusedExport && v.flat {
				say(stdout, "%s:%d:%d: %s is exported by module %s but no production code outside it uses it; move it to the module's test door or unexport it\n", v.file, v.line, v.col, v.pkg, v.module)
				continue
			}
			if v.kind == kindUnusedExport {
				say(stdout, "%s:%d:%d: %s is exported by module %s but no file outside it uses it; unexport it and let the module's own tests go through the real door\n", v.file, v.line, v.col, v.pkg, v.module)
				continue
			}
			say(stdout, "%s:%d: non-test code imports the test-support package %s; only _test.go files and other test-support packages may\n", v.file, v.line, v.pkg)
		}
	}
	say(stdout, "test-support imports from production code and unused root exports: %d\n", len(s.violations))
	if len(s.violations) > 0 {
		return 1
	}
	return 0
}

func modulePathOf(root string) (string, error) {
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
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

func isTestSupport(dir string) bool {
	return strings.HasSuffix(lastSegment(dir), "test")
}

func discoverModules(root string) (map[string]bool, error) {
	modules := map[string]bool{}
	top, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
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
			return nil, fmt.Errorf("read dir: %w", err)
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
		return nil, fmt.Errorf("walk dir: %w", err)
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
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	rel, err := filepath.Rel(s.root, filepath.Dir(file))
	if err != nil {
		return fmt.Errorf("rel: %w", err)
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
		v := violation{file: file, line: fset.Position(imp.Pos()).Line, pkg: target, module: module, importer: importer}
		switch {
		case isTestSupport(target) && !isTest && !isTestSupport(importer):
			v.kind = kindTestSupport
			s.violations = append(s.violations, v)
		case sub == "" && module != ownModule:
			s.recordUse(f, imp, module, outsideFlag(file, importer))
		case sub == "" && importer != "internal/"+module:
			s.recordUse(f, imp, module, usedInside)
		}
	}
	return nil
}

func outsideFlag(file, importer string) int {
	if strings.HasSuffix(file, "_test.go") || isTestSupport(importer) {
		return usedByTests
	}
	return usedByProduction
}

func (s *scan) recordUse(f *ast.File, imp *ast.ImportSpec, module string, flag int) {
	alias := s.pkgNames[module]
	if imp.Name != nil {
		alias = imp.Name.Name
	}
	if alias == "" || alias == "_" || alias == "." {
		return
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !sel.Sel.IsExported() {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == alias && id.Obj == nil {
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
		switch s.used[module][name] &^ usedInside {
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
		testSupport := 0
		for _, v := range s.violations {
			if v.module == m && v.kind == kindTestSupport {
				testSupport++
			}
		}
		say(w, "%s: %d exported at the root, %d used outside by production code, %d used outside only by tests, %d unused outside; %d production imports of test support\n",
			m, d.exported, d.prod, d.tests, d.exported-d.prod-d.tests, testSupport)
	}
	return nil
}

type declPos struct {
	file      string
	line, col int
}

func (s *scan) addUnusedExports() error {
	modules := make([]string, 0, len(s.modules))
	for m := range s.modules {
		modules = append(modules, m)
	}
	sort.Strings(modules)
	for _, m := range modules {
		names, err := rootExports(filepath.Join(s.root, "internal", filepath.FromSlash(m)))
		if err != nil {
			return err
		}
		flat, err := isFlattened(filepath.Join(s.root, "internal", filepath.FromSlash(m)), m)
		if err != nil {
			return err
		}
		for name, pos := range names {
			unused := s.used[m][name] == 0
			if flat {
				unused = s.used[m][name]&usedByProduction == 0
			}
			if unused {
				s.violations = append(s.violations, violation{file: pos.file, line: pos.line, col: pos.col, pkg: name, module: m, kind: kindUnusedExport, flat: flat})
			}
		}
	}
	sort.Slice(s.violations, func(i, j int) bool {
		a, b := s.violations[i], s.violations[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.col < b.col
	})
	return nil
}

func isFlattened(dir, module string) (bool, error) {
	if strings.HasPrefix(module, sharedGroup+"/") {
		return false, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
	}
	return len(files) == 1 && files[0] == lastSegment(module)+".go", nil
}

func rootExports(dir string) (map[string]declPos, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	names := map[string]declPos{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file := filepath.Join(dir, name)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		collectExported(fset, file, f, names)
	}
	return names, nil
}

func collectExported(fset *token.FileSet, file string, f *ast.File, names map[string]declPos) {
	add := func(id *ast.Ident) {
		if id.IsExported() {
			p := fset.Position(id.Pos())
			names[id.Name] = declPos{file: file, line: p.Line, col: p.Column}
		}
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch sp := spec.(type) {
				case *ast.TypeSpec:
					add(sp.Name)
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						add(n)
					}
				}
			}
		}
	}
}
