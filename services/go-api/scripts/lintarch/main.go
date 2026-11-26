package main

import (
	"encoding/json"
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

const usage = "usage: go run ./scripts/lintarch [--summary] [--rules <rule>[,<rule>...]] [<go-api dir>]\n  checks the module architecture declared in <go-api dir>/architecture.json and exits 1 on any violation; rules: role (every module has a role), edge (a module imports only the roles its role may), layer (core, service, domain, ports and shell packages import no I/O), driver (database driver packages only in adapters), table (only the owning module writes a table, and every written table has an owner); --summary prints the count per rule and module"

var rules = []string{"role", "edge", "layer", "driver", "table"}

type config struct {
	Roles         map[string]string   `json:"roles"`
	Groups        []string            `json:"groups"`
	MayImport     map[string][]string `json:"may_import"`
	PureLayers    []string            `json:"pure_layers"`
	IOImports     []string            `json:"io_imports"`
	DriverImports []string            `json:"driver_imports"`
	DriverHomes   driverHomes         `json:"driver_homes"`
	Composition   composition         `json:"driver_composition"`
	TableOwners   map[string]string   `json:"table_owners"`
}

type driverHomes struct {
	Segments []string `json:"segments"`
	Modules  []string `json:"modules"`
}

type composition struct {
	Dirs    []string `json:"dirs"`
	Imports []string `json:"imports"`
}

type violation struct {
	file   string
	line   int
	rule   string
	module string
	msg    string
}

type linter struct {
	cfg        config
	modulePath string
	root       string
	tables     map[string]bool
	found      []violation
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdout, stderr io.Writer) int {
	summary := false
	var only []string
	root := "."
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--summary":
			summary = true
		case arg == "--rules" && i+1 < len(args):
			i++
			only = strings.Split(args[i], ",")
		case strings.HasPrefix(arg, "-"):
			say(stderr, "%s\n", usage)
			return 2
		default:
			root = arg
		}
	}
	for _, r := range only {
		if !contains(rules, r) {
			say(stderr, "lintarch: unknown rule %q, rules are %s\n", r, strings.Join(rules, ", "))
			return 2
		}
	}
	l, err := newLinter(root)
	if err != nil {
		say(stderr, "lintarch: %v\n", err)
		return 2
	}
	if err := l.scan(); err != nil {
		say(stderr, "lintarch: %v\n", err)
		return 2
	}
	var shown []violation
	for _, v := range l.found {
		if len(only) == 0 || contains(only, v.rule) {
			shown = append(shown, v)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool {
		if shown[i].file != shown[j].file {
			return shown[i].file < shown[j].file
		}
		return shown[i].line < shown[j].line
	})
	if summary {
		printSummary(stdout, shown)
	} else {
		for _, v := range shown {
			say(stdout, "%s:%d: %s: %s\n", v.file, v.line, v.rule, v.msg)
		}
	}
	say(stdout, "architecture violations: %d\n", len(shown))
	if len(shown) > 0 {
		return 1
	}
	return 0
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func printSummary(w io.Writer, shown []violation) {
	counts := map[string]int{}
	for _, v := range shown {
		counts[v.rule+"\t"+v.module]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rule, module, _ := strings.Cut(k, "\t")
		say(w, "%6d  %-7s %s\n", counts[k], rule, module)
	}
}

func newLinter(root string) (*linter, error) {
	l := &linter{root: root, tables: map[string]bool{}}
	raw, err := os.ReadFile(filepath.Join(root, "architecture.json"))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l.cfg); err != nil {
		return nil, fmt.Errorf("architecture.json: %w", err)
	}
	for module, role := range l.cfg.Roles {
		if _, ok := l.cfg.MayImport[role]; !ok {
			return nil, fmt.Errorf("architecture.json: %s has role %s, which has no entry in may_import", module, role)
		}
	}
	l.modulePath, err = modulePathOf(root)
	if err != nil {
		return nil, err
	}
	if err := l.loadTables(); err != nil {
		return nil, err
	}
	return l, nil
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

var tableDDL = regexp.MustCompile(`(?i)\b(CREATE|DROP)\s+TABLE\s+(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(?:[a-z_]+\.)?"?([a-z_][a-z0-9_]*)"?`)

func (l *linter) loadTables() error {
	files, err := filepath.Glob(filepath.Join(l.root, "migrations", "*.sql"))
	if err != nil {
		return err
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		for _, m := range tableDDL.FindAllStringSubmatch(string(body), -1) {
			name := strings.ToLower(m[2])
			if strings.EqualFold(m[1], "DROP") {
				delete(l.tables, name)
			} else {
				l.tables[name] = true
			}
		}
	}
	return nil
}

func (l *linter) splitModule(rest string) (module, sub string) {
	parts := strings.Split(rest, "/")
	n := 1
	if contains(l.cfg.Groups, parts[0]) {
		n = 2
	}
	if len(parts) < n {
		return "", ""
	}
	return strings.Join(parts[:n], "/"), strings.Join(parts[n:], "/")
}

func (l *linter) roleOf(module string) string {
	top, _, _ := strings.Cut(module, "/")
	return l.cfg.Roles[top]
}

func (l *linter) scan() error {
	return filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", "node_modules", ".git", "migrations":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		return l.scanFile(p)
	})
}

var writeStatement = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|MERGE\s+INTO|TRUNCATE(?:\s+TABLE)?)\s+(?:ONLY\s+)?(?:[a-z_]+\.)?"?([a-z_][a-z0-9_]*)"?`)

func (l *linter) scanFile(file string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	rel, err := filepath.Rel(l.root, filepath.Dir(file))
	if err != nil {
		return err
	}
	dir := filepath.ToSlash(rel)
	ownModule, within := "", ""
	if rest, ok := strings.CutPrefix(dir, "internal/"); ok {
		ownModule, within = l.splitModule(rest)
		if ownModule == "" {
			l.add(file, 1, "role", dir, fmt.Sprintf("%s holds Go code that belongs to no module; put it in a module folder under %s", dir, dir))
			return nil
		}
	} else if dir == "internal" {
		l.add(file, 1, "role", dir, "internal holds Go code that belongs to no module; put it in a module folder")
		return nil
	} else if !strings.HasPrefix(dir, "cmd/") && dir != "cmd" {
		return nil
	}
	isTest := strings.HasSuffix(file, "_test.go")
	ownRole := ""
	if ownModule != "" {
		ownRole = l.roleOf(ownModule)
		if ownRole == "" {
			l.add(file, 1, "role", ownModule, fmt.Sprintf("module %s has no role in architecture.json; add it to roles", ownModule))
			return nil
		}
	}
	segments := strings.Split(within, "/")
	pure := !isTest && hasAny(segments, l.cfg.PureLayers) && !hasAny(segments, []string{"adapters"})
	driverHome := hasAny(segments, l.cfg.DriverHomes.Segments) || contains(l.cfg.DriverHomes.Modules, ownModule)
	wiring := matchesAny(dir, l.cfg.Composition.Dirs)
	prefix := l.modulePath + "/internal/"
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		line := fset.Position(imp.Pos()).Line
		if rest, ok := strings.CutPrefix(target, prefix); ok && ownModule != "" {
			l.checkEdge(file, line, ownModule, ownRole, rest)
		}
		if pure && matchesAny(target, l.cfg.IOImports) {
			l.add(file, line, "layer", ownModule, fmt.Sprintf("%s imports %s, I/O belongs in an adapter; core, service, domain, ports and shell packages stay pure", dir, target))
		}
		if !isTest && !driverHome && matchesAny(target, l.cfg.DriverImports) && (!wiring || !matchesAny(target, l.cfg.Composition.Imports)) {
			l.add(file, line, "driver", moduleLabel(ownModule, dir), fmt.Sprintf("%s imports the database driver %s outside an adapters package", dir, target))
		}
	}
	if !isTest {
		l.checkWrites(fset, file, f, moduleLabel(ownModule, dir), ownModule)
	}
	return nil
}

func moduleLabel(module, dir string) string {
	if module != "" {
		return module
	}
	return dir
}

func hasAny(segments, want []string) bool {
	for _, s := range segments {
		if contains(want, s) {
			return true
		}
	}
	return false
}

func matchesAny(target string, prefixes []string) bool {
	for _, p := range prefixes {
		if tree, ok := strings.CutSuffix(p, "/..."); ok {
			if target == tree || strings.HasPrefix(target, tree+"/") {
				return true
			}
		} else if target == p {
			return true
		}
	}
	return false
}

func (l *linter) add(file string, line int, rule, module, msg string) {
	l.found = append(l.found, violation{file: file, line: line, rule: rule, module: module, msg: msg})
}

func (l *linter) checkEdge(file string, line int, ownModule, ownRole, rest string) {
	targetModule, _ := l.splitModule(rest)
	if targetModule == "" || targetModule == ownModule {
		return
	}
	targetRole := l.roleOf(targetModule)
	if targetRole == "" {
		l.add(file, line, "role", targetModule, fmt.Sprintf("module %s has no role in architecture.json; add it to roles", targetModule))
		return
	}
	if !contains(l.cfg.MayImport[ownRole], targetRole) {
		l.add(file, line, "edge", ownModule, fmt.Sprintf("%s (%s) imports %s (%s); a %s may import only %s", ownModule, ownRole, targetModule, targetRole, ownRole, strings.Join(l.cfg.MayImport[ownRole], ", ")))
	}
}

func (l *linter) checkWrites(fset *token.FileSet, file string, f *ast.File, label, ownModule string) {
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, m := range writeStatement.FindAllStringSubmatch(text, -1) {
			table := strings.ToLower(m[1])
			if !l.tables[table] {
				continue
			}
			line := fset.Position(lit.Pos()).Line
			owner, owned := l.cfg.TableOwners[table]
			switch {
			case !owned:
				l.add(file, line, "table", label, fmt.Sprintf("%s writes table %s, which has no owner in architecture.json; add it to table_owners", label, table))
			case owner != ownModule:
				l.add(file, line, "table", label, fmt.Sprintf("%s writes table %s, which module %s owns; ask %s through its door", label, table, owner, owner))
			}
		}
		return true
	})
}
