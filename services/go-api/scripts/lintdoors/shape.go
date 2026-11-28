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
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const appModule = "app"

const readmeFile = "services/go-api/README.md"

const (
	sectionRoot    = "Rule 1: one shape per module"
	sectionDoor    = "Rule 2: one door file"
	sectionCore    = "Rule 5: the inside imports nothing from adapters"
	sectionLayers  = "Rule 7: one package per layer"
	sectionFileMap = "Rule 10: one file map"
	sectionSetters = "Rule 13: complete at construction"
	sectionGlobals = "Rule 18: no package-level mutable state"
	sectionNames   = "Rule 20: package names are one plain word"
	sectionReadme  = "Rule 21: every module README opens with its one job"
)

var allSections = []string{sectionRoot, sectionDoor, sectionCore, sectionLayers, sectionFileMap, sectionSetters, sectionGlobals, sectionNames, sectionReadme}

var layerDirs = map[string]bool{"domain": true, "service": true, "ports": true, "contracttest": true, "adapters": true}

var bannedPackageNames = map[string]bool{"util": true, "utils": true, "common": true, "helpers": true, "helper": true, "misc": true}

var plainWord = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

var stdlibWithMethods = map[string]bool{"WithAttrs": true, "WithGroup": true}

const minReadmeWords = 4

type shapeViolation struct {
	path    string
	line    int
	message string
	section string
}

func (v shapeViolation) String() string {
	where := v.path
	if v.line > 0 {
		where = v.path + ":" + strconv.Itoa(v.line)
	}
	return fmt.Sprintf("%s: %s; read %s, section %q", where, v.message, readmeFile, v.section)
}

type shapeScan struct {
	root       string
	modulePath string
	found      []shapeViolation
}

func (s *shapeScan) add(path string, line int, section, format string, a ...any) {
	s.found = append(s.found, shapeViolation{path: path, line: line, section: section, message: fmt.Sprintf(format, a...)})
}

func checkShape(root string) ([]shapeViolation, error) {
	modulePath, err := modulePathOf(root)
	if err != nil {
		return nil, err
	}
	modules, err := discoverModules(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(modules))
	for m := range modules {
		if m != appModule && !strings.HasPrefix(m, sharedGroup+"/") {
			names = append(names, m)
		}
	}
	sort.Strings(names)
	s := &shapeScan{root: root, modulePath: modulePath}
	for _, m := range names {
		if err := s.checkModule(m); err != nil {
			return nil, err
		}
	}
	if err := s.checkPackageNames(); err != nil {
		return nil, err
	}
	sort.SliceStable(s.found, func(i, j int) bool {
		a, b := s.found[i], s.found[j]
		if a.path != b.path {
			return a.path < b.path
		}
		return a.line < b.line
	})
	return s.found, nil
}

func listDir(dir string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	return entries, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (s *shapeScan) checkModule(module string) error {
	dir := filepath.Join(s.root, "internal", filepath.FromSlash(module))
	name := lastSegment(module)
	if err := s.checkRootShape(dir, name); err != nil {
		return err
	}
	if err := s.checkLayers(filepath.Join(dir, "internal")); err != nil {
		return err
	}
	s.checkFileMap(filepath.Join(dir, "internal"))
	s.checkReadme(dir, name)
	if err := s.checkGlobals(dir); err != nil {
		return err
	}
	return s.checkSources(dir, name)
}

func (s *shapeScan) checkRootShape(dir, name string) error {
	entries, err := listDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		n := e.Name()
		path := filepath.Join(dir, n)
		switch {
		case e.IsDir() && (n == "internal" || n == name+"test"):
		case e.IsDir():
			s.add(path, 0, sectionRoot, "folder %s at the module root; the root holds only %s.go, README.md, CLAUDE.md, internal/ and an optional %stest/, so move it under internal/", n, name, name)
		case n == name+".go" || n == "README.md" || n == "CLAUDE.md" || n == name+"_test.go":
		case strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go"):
			s.add(path, 0, sectionDoor, "second door file %s; the door is the one file %s.go, so fold this into it or move it under internal/", n, name)
		default:
			s.add(path, 0, sectionRoot, "file %s at the module root; the root holds only %s.go, README.md, CLAUDE.md, internal/ and an optional %stest/", n, name, name)
		}
	}
	if !isDir(filepath.Join(dir, "internal")) {
		s.add(dir, 0, sectionRoot, "module %s has no internal/ folder", name)
	}
	door := filepath.Join(dir, name+".go")
	if !isFile(door) {
		s.add(door, 0, sectionDoor, "module %s has no door file %s.go", name, name)
		return nil
	}
	f, err := parser.ParseFile(token.NewFileSet(), door, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", door, err)
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "New" {
			return nil
		}
	}
	s.add(door, 0, sectionDoor, "the door %s.go declares no func New", name)
	return nil
}

func (s *shapeScan) checkLayers(internal string) error {
	entries, err := listDir(internal)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(internal, e.Name())
		switch {
		case !e.IsDir():
			s.add(path, 0, sectionLayers, "file %s directly in internal/; code lives in a layer package: domain, service, ports, contracttest or adapters/<outside system>", e.Name())
		case !layerDirs[e.Name()]:
			s.add(path, 0, sectionLayers, "package folder %s under internal/; the layers are domain, service, ports, contracttest and adapters, so a new concept is a new file in one of them", e.Name())
		case e.Name() == "adapters":
			if err := s.checkAdapterDirs(path); err != nil {
				return err
			}
		default:
			if err := s.checkNoNesting(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *shapeScan) checkAdapterDirs(adapters string) error {
	entries, err := listDir(adapters)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(adapters, e.Name())
		if !e.IsDir() {
			s.add(path, 0, sectionLayers, "file %s directly in adapters/; each outside system has its own folder adapters/<system>", e.Name())
			continue
		}
		if err := s.checkNoNesting(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *shapeScan) checkNoNesting(pkg string) error {
	entries, err := listDir(pkg)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != "testdata" {
			s.add(filepath.Join(pkg, e.Name()), 0, sectionLayers, "package folder nested below %s; a new concept is a new file in %s, not a new package", filepath.Base(pkg), filepath.Base(pkg))
		}
	}
	return nil
}

func (s *shapeScan) checkFileMap(internal string) {
	domain := filepath.Join(internal, "domain")
	if isDir(domain) && !isFile(filepath.Join(domain, "errors.go")) {
		s.add(domain, 0, sectionFileMap, "domain/ has no errors.go; every business error of the module is declared there")
	}
	ports := filepath.Join(internal, "ports")
	if isDir(ports) && !isFile(filepath.Join(ports, "ports.go")) {
		s.add(ports, 0, sectionFileMap, "ports/ has no ports.go; every interface the module needs from outside is declared there")
	}
	handler := filepath.Join(internal, "adapters", "handler")
	if isDir(handler) {
		routes, wire := handlerNeeds(handler)
		wanted := map[string]bool{"errors.go": true, "routes.go": routes, "dto.go": wire}
		for _, want := range []string{"routes.go", "dto.go", "errors.go"} {
			if wanted[want] && !isFile(filepath.Join(handler, want)) {
				s.add(handler, 0, sectionFileMap, "adapters/handler/ has no %s; routes, wire shapes and error mapping each have one file there", want)
			}
		}
	}
	s.checkRepoPairs(internal)
}

func handlerNeeds(handler string) (routes, wire bool) {
	entries, _ := listDir(handler)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(handler, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, imp := range f.Imports {
			if strings.HasPrefix(imp.Path.Value, `"github.com/go-chi/chi`) {
				routes = true
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if field, ok := n.(*ast.Field); ok && field.Tag != nil && strings.Contains(field.Tag.Value, `json:"`) {
				wire = true
			}
			return true
		})
	}
	return routes, wire
}

func (s *shapeScan) checkRepoPairs(internal string) {
	persistence := filepath.Join(internal, "adapters", "persistence")
	entries, _ := listDir(persistence)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, "_repo.go") {
			continue
		}
		if !isFile(filepath.Join(internal, "adapters", "memory", n)) {
			s.add(filepath.Join(persistence, n), 0, sectionFileMap, "adapters/persistence/%s has no adapters/memory/%s beside it; every table repository has a memory fake", n, n)
		}
	}
}

func (s *shapeScan) checkReadme(dir, name string) {
	path := filepath.Join(dir, "README.md")
	body, err := os.ReadFile(path)
	if err != nil {
		s.add(path, 0, sectionReadme, "module %s has no README.md", name)
		return
	}
	first, _, _ := strings.Cut(string(body), "\n")
	first = strings.TrimSpace(first)
	if !isSentence(first) {
		s.add(path, 1, sectionReadme, "line 1 is not a sentence stating the module's one job (%q)", first)
	}
}

func isSentence(line string) bool {
	if strings.ContainsAny(line[:min(1, len(line))], "#-*|<>[!>") {
		return false
	}
	return strings.HasSuffix(line, ".") && len(strings.Fields(line)) >= minReadmeWords
}

func (s *shapeScan) checkSources(dir, name string) error {
	apierrorPath := s.modulePath + "/internal/shared/apierror"
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return fmt.Errorf("rel: %w", err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		segs := strings.Split(filepath.ToSlash(rel), "/")
		s.checkFile(p, segs, fset, f, apierrorPath)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}
	return nil
}

func (s *shapeScan) checkFile(path string, segs []string, fset *token.FileSet, f *ast.File, apierrorPath string) {
	inAdapter := len(segs) >= 4 && segs[0] == "internal" && segs[1] == "adapters"
	inHandler := inAdapter && segs[2] == "handler"
	inCore := len(segs) >= 3 && segs[0] == "internal" && (segs[1] == "domain" || segs[1] == "service" || segs[1] == "ports")
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err == nil && inCore && strings.Contains(target, "/internal/adapters/") {
			s.add(path, fset.Position(imp.Pos()).Line, sectionCore, "%s imports an adapter (%s); the inside never names the outside, so reach it through a port", segs[1], target)
		}
		if err == nil && target == apierrorPath && !inHandler {
			s.add(path, fset.Position(imp.Pos()).Line, sectionFileMap, "imports apierror outside adapters/handler; only the handler maps domain errors to HTTP, so return a domain error from here and map it in adapters/handler/errors.go")
		}
	}
	jsonAllowed := inAdapter && (!inHandler || segs[len(segs)-1] == "dto.go")
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			if x.Tag != nil && strings.Contains(x.Tag.Value, `json:"`) && !jsonAllowed {
				s.add(path, fset.Position(x.Tag.Pos()).Line, sectionFileMap, "json tag outside a wire or stored shape; wire shapes live in adapters/handler/dto.go and stored shapes in their own adapter, never in domain, service, ports or the door")
			}
		case *ast.FuncDecl:
			if isWithSetter(x) {
				s.add(path, fset.Position(x.Name.Pos()).Line, sectionSetters, "exported method %s changes a built value; a dependency or setting comes in through New(Deps), never through a setter after construction", x.Name.Name)
			}
		}
		return true
	})
}

type pkgVar struct {
	name  string
	spec  *ast.ValueSpec
	ident *ast.Ident
	file  string
	line  int
}

func (s *shapeScan) checkGlobals(dir string) error {
	byDir := map[string][]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			byDir[filepath.Dir(p)] = append(byDir[filepath.Dir(p)], p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}
	pkgs := map[string]*parsedPackage{}
	for pkgDir, paths := range byDir {
		pkg, err := parsePackage(paths)
		if err != nil {
			return err
		}
		pkgs[filepath.Clean(pkgDir)] = pkg
	}
	changed := map[string]map[string]bool{}
	for pkgDir := range pkgs {
		changed[pkgDir] = map[string]bool{}
	}
	for pkgDir, pkg := range pkgs {
		for _, f := range pkg.files {
			s.collectChanged(f, pkgDir, pkgs, changed)
		}
	}
	for pkgDir, pkg := range pkgs {
		for _, v := range pkg.vars {
			switch {
			case holdsSyncState(v.spec):
				s.add(v.file, v.line, sectionGlobals, "package-level %s holds runtime state (a lock, a once or an atomic); state lives in what New builds", v.name)
			case changed[pkgDir][v.name]:
				s.add(v.file, v.line, sectionGlobals, "package-level var %s is assigned or written to in code; state lives in what New builds", v.name)
			}
		}
	}
	return nil
}

type parsedPackage struct {
	files []*ast.File
	vars  []pkgVar
}

func parsePackage(paths []string) (*parsedPackage, error) {
	pkg := &parsedPackage{}
	for _, p := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		pkg.files = append(pkg.files, f)
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for _, id := range vs.Names {
					if id.Name != "_" {
						pkg.vars = append(pkg.vars, pkgVar{name: id.Name, spec: vs, ident: id, file: p, line: fset.Position(id.Pos()).Line})
					}
				}
			}
		}
	}
	return pkg, nil
}

func (s *shapeScan) importedDirs(f *ast.File, pkgs map[string]*parsedPackage) map[string]string {
	byAlias := map[string]string{}
	prefix := s.modulePath + "/"
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(target, prefix) {
			continue
		}
		pkgDir := filepath.Clean(filepath.Join(s.root, filepath.FromSlash(strings.TrimPrefix(target, prefix))))
		if _, ok := pkgs[pkgDir]; !ok {
			continue
		}
		alias := packageName(pkgDir)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		byAlias[alias] = pkgDir
	}
	return byAlias
}

func (s *shapeScan) collectChanged(f *ast.File, pkgDir string, pkgs map[string]*parsedPackage, changed map[string]map[string]bool) {
	own := map[string]*ast.ValueSpec{}
	for _, v := range pkgs[pkgDir].vars {
		own[v.name] = v.spec
	}
	imported := s.importedDirs(f, pkgs)
	mark := func(e ast.Expr) {
		for {
			switch x := e.(type) {
			case *ast.Ident:
				if spec, ok := own[x.Name]; ok && (x.Obj == nil || x.Obj.Decl == spec) {
					changed[pkgDir][x.Name] = true
				}
				return
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Obj == nil {
					if target, isImport := imported[id.Name]; isImport {
						changed[target][x.Sel.Name] = true
						return
					}
				}
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.StarExpr:
				e = x.X
			case *ast.ParenExpr:
				e = x.X
			default:
				return
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, l := range x.Lhs {
					mark(l)
				}
			}
		case *ast.IncDecStmt:
			mark(x.X)
		case *ast.CallExpr:
			if fun, ok := x.Fun.(*ast.Ident); ok && (fun.Name == "delete" || fun.Name == "clear") && len(x.Args) > 0 {
				mark(x.Args[0])
			}
		}
		return true
	})
}

func holdsSyncState(spec *ast.ValueSpec) bool {
	found := false
	check := func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "sync" || pkg.Name == "atomic") {
			found = true
		}
		return true
	}
	if spec.Type != nil {
		ast.Inspect(spec.Type, check)
	}
	for _, v := range spec.Values {
		if _, isFunc := v.(*ast.FuncLit); !isFunc {
			ast.Inspect(v, check)
		}
	}
	return found
}

func isWithSetter(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	if _, pointer := fn.Recv.List[0].Type.(*ast.StarExpr); !pointer {
		return false
	}
	name := fn.Name.Name
	rest, ok := strings.CutPrefix(name, "With")
	if !ok || stdlibWithMethods[name] {
		return false
	}
	return rest == "" || (rest[0] >= 'A' && rest[0] <= 'Z')
}

func (s *shapeScan) checkPackageNames() error {
	internal := filepath.Join(s.root, "internal")
	err := filepath.WalkDir(internal, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == "testdata" {
			return filepath.SkipDir
		}
		pkg := packageName(p)
		if pkg == "" || pkg == "main" {
			return nil
		}
		s.checkName(p, filepath.Base(p), "folder")
		if pkg != filepath.Base(p) {
			s.checkName(p, pkg, "package")
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}
	return nil
}

func (s *shapeScan) checkName(path, name, what string) {
	switch {
	case bannedPackageNames[name]:
		s.add(path, 0, sectionNames, "%s name %q says nothing about what it holds; name it for what it is, or copy the small helper where it is used", what, name)
	case !plainWord.MatchString(name):
		s.add(path, 0, sectionNames, "%s name %q is not one plain lowercase word", what, name)
	}
}

func runShape(root string, stdout, stderr io.Writer) int {
	found, err := checkShape(root)
	if err != nil {
		say(stderr, "lintdoors: %v\n", err)
		return 2
	}
	for _, v := range found {
		say(stdout, "%s\n", v)
	}
	say(stdout, "module shape violations: %d\n", len(found))
	if len(found) > 0 {
		return 1
	}
	return 0
}
