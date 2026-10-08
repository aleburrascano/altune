package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	ruleRuntimeMethod  = "runtime-method"
	ruleRecover        = "recover-call"
	ruleSelectInFor    = "select-in-for"
	ruleSyncStateField = "sync-state-field"
)

var wiringOnlyAllowlist = map[string]string{
	"leader_ticker.go":  "leader ticker with recover and a for/select loop",
	"jobs.go":           "job runner holding mutex and atomic state",
	"app.go":            "App struct holding the jobsMu mutex",
	"health.go":         "health checker holding a mutex",
	"catalog_wiring.go": "lookupTracker holding an atomic counter",
}

func wiringViolations(fset *token.FileSet, files map[string]*ast.File) map[string][]string {
	_ = fset
	out := map[string][]string{}
	for name, f := range files {
		set := map[string]bool{}
		syncNames, atomicNames := importNames(f)
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Recv != nil && (n.Name.Name == "ServeHTTP" || n.Name.Name == "RoundTrip") {
					set[ruleRuntimeMethod] = true
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "recover" && id.Obj == nil {
					set[ruleRecover] = true
				}
			case *ast.ForStmt:
				if containsSelect(n.Body) {
					set[ruleSelectInFor] = true
				}
			case *ast.RangeStmt:
				if containsSelect(n.Body) {
					set[ruleSelectInFor] = true
				}
			case *ast.StructType:
				for _, fld := range n.Fields.List {
					if isSyncStateType(fld.Type, syncNames, atomicNames) {
						set[ruleSyncStateField] = true
					}
				}
			}
			return true
		})
		if len(set) == 0 {
			continue
		}
		rules := make([]string, 0, len(set))
		for r := range set {
			rules = append(rules, r)
		}
		sort.Strings(rules)
		out[name] = rules
	}
	return out
}

func importNames(f *ast.File) (syncNames, atomicNames map[string]bool) {
	syncNames, atomicNames = map[string]bool{}, map[string]bool{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		var target map[string]bool
		var local string
		switch path {
		case "sync":
			target, local = syncNames, "sync"
		case "sync/atomic":
			target, local = atomicNames, "atomic"
		default:
			continue
		}
		if imp.Name != nil {
			local = imp.Name.Name
		}
		target[local] = true
	}
	return syncNames, atomicNames
}

func containsSelect(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.SelectStmt:
			found = true
		}
		return !found
	})
	return found
}

func isSyncStateType(expr ast.Expr, syncNames, atomicNames map[string]bool) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return isSyncStateType(t.X, syncNames, atomicNames)
	case *ast.IndexExpr:
		return isSyncStateType(t.X, syncNames, atomicNames)
	case *ast.IndexListExpr:
		return isSyncStateType(t.X, syncNames, atomicNames)
	case *ast.ArrayType:
		return isSyncStateType(t.Elt, syncNames, atomicNames)
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}
		if syncNames[pkg.Name] {
			return t.Sel.Name == "Mutex" || t.Sel.Name == "RWMutex"
		}
		return atomicNames[pkg.Name]
	}
	return false
}

func parseSources(t *testing.T, srcs map[string]string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for name, src := range srcs {
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = f
	}
	return fset, files
}

func parseAppPackage(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files[name] = f
	}
	return fset, files
}

func TestWiringOnly_PackageHasNoRuntimeOutsideAllowlist(t *testing.T) {
	fset, files := parseAppPackage(t)
	got := wiringViolations(fset, files)

	for name, rules := range got {
		if _, ok := wiringOnlyAllowlist[name]; !ok {
			t.Errorf("%s holds runtime code (%s) but is not on the wiring-only allowlist; move it out of internal/app", name, strings.Join(rules, ", "))
		}
	}
	for name, reason := range wiringOnlyAllowlist {
		if _, ok := files[name]; !ok {
			t.Errorf("allowlist entry %s (%s) names a file that no longer exists; remove it", name, reason)
			continue
		}
		if len(got[name]) == 0 {
			t.Errorf("allowlist entry %s (%s) no longer violates anything; remove it", name, reason)
		}
	}
}

func TestWiringOnly_ViolatingFilesEqualAllowlist(t *testing.T) {
	fset, files := parseAppPackage(t)
	var got, want []string
	for name := range wiringViolations(fset, files) {
		got = append(got, name)
	}
	for name := range wiringOnlyAllowlist {
		want = append(want, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("files with violations = %v, want exactly the allowlist %v", got, want)
	}
	if len(want) != 5 {
		t.Errorf("allowlist has %d entries, want 5 at the start", len(want))
	}
}

func TestWiringOnly_Rules(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"empty file", "package app\n", nil},
		{"ServeHTTP method", "package app\nimport \"net/http\"\ntype x struct{}\nfunc (h *x) ServeHTTP(w http.ResponseWriter, r *http.Request) {}\n", []string{ruleRuntimeMethod}},
		{"RoundTrip method", "package app\nimport \"net/http\"\ntype x struct{}\nfunc (h *x) RoundTrip(r *http.Request) (*http.Response, error) { return nil, nil }\n", []string{ruleRuntimeMethod}},
		{"next.ServeHTTP call", "package app\nimport \"net/http\"\nfunc mw(next http.Handler) http.Handler {\n\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })\n}\n", nil},
		{"serveHTTPLater function", "package app\nfunc serveHTTPLater() {}\n", nil},
		{"sync.WaitGroup field", "package app\nimport \"sync\"\ntype x struct{ wg sync.WaitGroup }\n", nil},
		{"sync.OnceValue", "package app\nimport \"sync\"\nvar v = sync.OnceValue(func() int { return 1 })\n", nil},
		{"select outside for", "package app\nfunc f(c chan int) {\n\tselect {\n\tcase <-c:\n\t}\n}\n", nil},
		{"method named recover", "package app\ntype x struct{}\nfunc (x) recover() {}\n", nil},
		{"recover call", "package app\nfunc f() {\n\tdefer func() { _ = recover() }()\n}\n", []string{ruleRecover}},
		{"for select", "package app\nfunc f() {\n\tfor {\n\t\tselect {}\n\t}\n}\n", []string{ruleSelectInFor}},
		{"for if select", "package app\nfunc f(ok bool) {\n\tfor {\n\t\tif ok {\n\t\t\tselect {}\n\t\t}\n\t}\n}\n", []string{ruleSelectInFor}},
		{"range select", "package app\nfunc f(xs []int) {\n\tfor range xs {\n\t\tselect {}\n\t}\n}\n", []string{ruleSelectInFor}},
		{"sync.Mutex field", "package app\nimport \"sync\"\ntype x struct{ mu sync.Mutex }\n", []string{ruleSyncStateField}},
		{"sync.RWMutex pointer field", "package app\nimport \"sync\"\ntype x struct{ mu *sync.RWMutex }\n", []string{ruleSyncStateField}},
		{"embedded Mutex", "package app\nimport \"sync\"\ntype x struct{ sync.Mutex }\n", []string{ruleSyncStateField}},
		{"atomic.Int64 field", "package app\nimport \"sync/atomic\"\ntype x struct{ n atomic.Int64 }\n", []string{ruleSyncStateField}},
		{"atomic.Pointer generic field", "package app\nimport \"sync/atomic\"\ntype x struct{ p atomic.Pointer[int] }\n", []string{ruleSyncStateField}},
		{"aliased sync import", "package app\nimport s \"sync\"\ntype x struct{ mu s.Mutex }\n", []string{ruleSyncStateField}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset, files := parseSources(t, map[string]string{"f.go": tc.src})
			got := wiringViolations(fset, files)["f.go"]
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("violations = %v, want %v", got, tc.want)
			}
		})
	}
}
