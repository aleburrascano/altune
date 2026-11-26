package collabcheck

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "collabcheck",
	Doc:  "reports a nil check on an interface or func field a constructor sets, and a With* option whose argument is an interface or func collaborator",
	Run:  run,

	ResultType: reflect.TypeFor[struct{}](),
}

func run(pass *analysis.Pass) (any, error) {
	files := productionFiles(pass)
	constructed := constructorSetFields(pass, files)
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BinaryExpr:
				reportNilCheck(pass, constructed, n)
			case *ast.FuncDecl:
				reportOption(pass, n)
			}
			return true
		})
	}
	return struct{}{}, nil
}

func productionFiles(pass *analysis.Pass) []*ast.File {
	var files []*ast.File
	for _, f := range pass.Files {
		if !strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			files = append(files, f)
		}
	}
	return files
}

func isCollaborator(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Signature:
		return true
	case *types.Interface:
		return u.NumMethods() > 0 && !types.Identical(t, types.Universe.Lookup("error").Type())
	}
	return false
}

func constructorSetFields(pass *analysis.Pass, files []*ast.File) map[*types.Var]bool {
	set := map[*types.Var]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil || !returnsNamedStruct(pass, fn) {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.KeyValueExpr:
					markKey(pass, set, n.Key)
				case *ast.AssignStmt:
					for _, lhs := range n.Lhs {
						markSelector(pass, set, lhs)
					}
				}
				return true
			})
		}
	}
	return set
}

func returnsNamedStruct(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, res := range fn.Type.Results.List {
		t := pass.TypesInfo.TypeOf(res.Type)
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if named, ok := t.(*types.Named); ok {
			if _, isStruct := named.Underlying().(*types.Struct); isStruct && named.Obj().Pkg() == pass.Pkg {
				return true
			}
		}
	}
	return false
}

func markKey(pass *analysis.Pass, set map[*types.Var]bool, key ast.Expr) {
	id, ok := key.(*ast.Ident)
	if !ok {
		return
	}
	if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && v.IsField() {
		set[v] = true
	}
}

func markSelector(pass *analysis.Pass, set map[*types.Var]bool, expr ast.Expr) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if v, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var); ok && v.IsField() {
		set[v] = true
	}
}

func reportNilCheck(pass *analysis.Pass, constructed map[*types.Var]bool, n *ast.BinaryExpr) {
	if n.Op != token.EQL && n.Op != token.NEQ {
		return
	}
	subject := n.X
	if isNil(pass, n.X) {
		subject = n.Y
	} else if !isNil(pass, n.Y) {
		return
	}
	sel, ok := ast.Unparen(subject).(*ast.SelectorExpr)
	if !ok {
		return
	}
	field, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !ok || !field.IsField() || field.Exported() || !constructed[field] || !isCollaborator(field.Type()) {
		return
	}
	pass.Reportf(n.Pos(), "nil check on collaborator field %s that a constructor sets: make it a required constructor parameter and refuse nil there", field.Name())
}

func isNil(pass *analysis.Pass, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	_, isNilObj := pass.TypesInfo.Uses[id].(*types.Nil)
	return isNilObj
}

func reportOption(pass *analysis.Pass, fn *ast.FuncDecl) {
	if !isOptionName(fn.Name.Name) || fn.Type.Params == nil || !returnsOption(pass, fn) {
		return
	}
	for _, param := range fn.Type.Params.List {
		if isCollaborator(pass.TypesInfo.TypeOf(param.Type)) {
			pass.Reportf(param.Pos(), "option %s takes a collaborator: make it a required constructor parameter", fn.Name.Name)
		}
	}
}

func returnsOption(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	var receiver types.Type
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		receiver = derefType(pass.TypesInfo.TypeOf(fn.Recv.List[0].Type))
	}
	for _, res := range fn.Type.Results.List {
		t := pass.TypesInfo.TypeOf(res.Type)
		if _, isFunc := t.Underlying().(*types.Signature); isFunc {
			return true
		}
		if receiver != nil && types.Identical(derefType(t), receiver) {
			return true
		}
	}
	return false
}

func derefType(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func isOptionName(name string) bool {
	rest, ok := strings.CutPrefix(name, "With")
	if !ok || rest == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return unicode.IsUpper(r)
}
