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

const usage = "usage: go run ./scripts/lintarch [--summary] [<go-api dir>]\n  checks that only the module that owns a table writes it, and that every written table has an owner, as declared in <go-api dir>/architecture.json (table_owners), and exits 1 on any violation; --summary prints the count per module"

type config struct {
	Groups      []string          `json:"groups"`
	TableOwners map[string]string `json:"table_owners"`
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
	l, err := newLinter(root)
	if err != nil {
		say(stderr, "lintarch: %v\n", err)
		return 2
	}
	if err := l.scan(); err != nil {
		say(stderr, "lintarch: %v\n", err)
		return 2
	}
	shown := l.found
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
		counts[v.module]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		say(w, "%6d  %s\n", counts[k], k)
	}
}

func newLinter(root string) (*linter, error) {
	l := &linter{root: root, tables: map[string]bool{}}
	raw, err := os.ReadFile(filepath.Join(root, "architecture.json"))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l.cfg); err != nil {
		return nil, fmt.Errorf("architecture.json: %w", err)
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
		return "", fmt.Errorf("read file: %w", err)
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
		return fmt.Errorf("glob: %w", err)
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
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

func (l *linter) scan() error {
	if err := filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
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
	}); err != nil {
		return fmt.Errorf("walk dir: %w", err)
	}
	return nil
}

var writeStatement = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|MERGE\s+INTO|TRUNCATE(?:\s+TABLE)?)\s+(?:ONLY\s+)?(?:[a-z_]+\.)?"?([a-z_][a-z0-9_]*)"?`)

func (l *linter) scanFile(file string) error {
	if strings.HasSuffix(file, "_test.go") {
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	rel, err := filepath.Rel(l.root, filepath.Dir(file))
	if err != nil {
		return fmt.Errorf("rel: %w", err)
	}
	dir := filepath.ToSlash(rel)
	ownModule := ""
	if rest, ok := strings.CutPrefix(dir, "internal/"); ok {
		ownModule, _ = l.splitModule(rest)
	} else if !strings.HasPrefix(dir, "cmd/") && dir != "cmd" {
		return nil
	}
	l.checkWrites(fset, file, f, moduleLabel(ownModule, dir), ownModule)
	return nil
}

func moduleLabel(module, dir string) string {
	if module != "" {
		return module
	}
	return dir
}

func (l *linter) add(file string, line int, rule, module, msg string) {
	l.found = append(l.found, violation{file: file, line: line, rule: rule, module: module, msg: msg})
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
				l.add(file, line, "table", label, fmt.Sprintf("%s writes table %s, which has no owner in architecture.json; add it to table_owners there and to the README of the module that writes it", label, table))
			case owner != ownModule:
				l.add(file, line, "table", label, fmt.Sprintf("%s writes table %s, which module %s owns; ask %s through its door", label, table, owner, owner))
			}
		}
		return true
	})
}
