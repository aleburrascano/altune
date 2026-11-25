package decodedfields

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type goPackage struct {
	structs      map[string]*declaredStruct
	pathTypes    map[string]bool
	pathLiterals map[string][]string
	lookups      map[string]bool
	assigned     map[ast.Expr]bool
}

type declaredStruct struct {
	body         *ast.StructType
	typeParams   []string
	declarations int
}

func readPackage(dirs []string) (*goPackage, error) {
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no source folders given")
	}
	pkg := &goPackage{
		structs:      map[string]*declaredStruct{},
		pathTypes:    map[string]bool{},
		pathLiterals: map[string][]string{},
		lookups:      map[string]bool{},
		assigned:     map[ast.Expr]bool{},
	}
	for _, dir := range dirs {
		if err := pkg.readDir(dir); err != nil {
			return nil, err
		}
	}
	return pkg, nil
}

func (p *goPackage) readDir(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return fmt.Errorf("list Go source in %s: %w", dir, err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no Go source in %s", dir)
	}
	for _, file := range files {
		if err := p.readFile(file); err != nil {
			return err
		}
	}
	return nil
}

func (p *goPackage) readFile(file string) error {
	if strings.HasSuffix(file, "_test.go") {
		return nil
	}
	text, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), file, text, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	ast.Inspect(parsed, p.learn)
	return nil
}

func (p *goPackage) learn(node ast.Node) bool {
	switch n := node.(type) {
	case *ast.TypeSpec:
		p.learnType(n)
	case *ast.FuncDecl:
		p.learnLocalStructs(n)
	case *ast.AssignStmt:
		p.learnAssignments(n)
	case *ast.IndexExpr:
		p.learnLookup(n)
	case *ast.CompositeLit:
		p.learnPathLiteral(n)
	}
	return true
}

func (p *goPackage) learnType(spec *ast.TypeSpec) {
	switch t := spec.Type.(type) {
	case *ast.StructType:
		p.declare(spec.Name.Name, t, spec.TypeParams)
	case *ast.ArrayType:
		p.pathTypes[spec.Name.Name] = true
	}
}

func (p *goPackage) learnLocalStructs(fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if spec, ok := node.(*ast.ValueSpec); ok {
			p.declareLocal(fn, spec)
		}
		return true
	})
}

func (p *goPackage) declareLocal(fn *ast.FuncDecl, spec *ast.ValueSpec) {
	body, ok := spec.Type.(*ast.StructType)
	if !ok {
		return
	}
	for _, name := range spec.Names {
		p.declare(fn.Name.Name+"."+name.Name, body, fn.Type.TypeParams)
	}
}

func (p *goPackage) declare(name string, body *ast.StructType, typeParams *ast.FieldList) {
	declared := p.structs[name]
	if declared == nil {
		declared = &declaredStruct{}
		p.structs[name] = declared
	}
	declared.body, declared.typeParams = body, paramNames(typeParams)
	declared.declarations++
}

func (p *goPackage) learnAssignments(assign *ast.AssignStmt) {
	for _, target := range assign.Lhs {
		p.assigned[target] = true
	}
}

func (p *goPackage) learnLookup(index *ast.IndexExpr) {
	if key, ok := stringLiteral(index.Index); ok && !p.assigned[index] {
		p.lookups[key] = true
	}
}

func (p *goPackage) learnPathLiteral(lit *ast.CompositeLit) {
	typeName, ok := lit.Type.(*ast.Ident)
	if !ok {
		return
	}
	for _, element := range lit.Elts {
		if key, ok := stringLiteral(element); ok {
			p.pathLiterals[typeName.Name] = append(p.pathLiterals[typeName.Name], key)
		}
	}
}

func (p *goPackage) lookedUpKeys() map[string]bool {
	keys := map[string]bool{}
	for key := range p.lookups {
		keys[key] = true
	}
	for typeName, literals := range p.pathLiterals {
		if !p.pathTypes[typeName] {
			continue
		}
		for _, key := range literals {
			keys[key] = true
		}
	}
	return keys
}

func paramNames(list *ast.FieldList) []string {
	if list == nil {
		return nil
	}
	var names []string
	for _, field := range list.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}
