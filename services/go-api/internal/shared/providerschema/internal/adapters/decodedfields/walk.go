package decodedfields

import (
	"altune/go-api/internal/shared/providerschema/internal/core/schema"
	"cmp"
	"go/ast"
	"reflect"
	"strings"
)

const maxSelfNesting = 2

type walker struct {
	pkg      *goPackage
	typeArgs map[string]string
	open     map[*ast.StructType]int
	reads    schema.Reads
}

func walk(pkg *goPackage, root *declaredStruct, typeArgs map[string]string) schema.Reads {
	w := walker{
		pkg:      pkg,
		typeArgs: typeArgs,
		open:     map[*ast.StructType]int{},
		reads:    schema.Reads{Paths: map[string]bool{}, Keys: pkg.lookedUpKeys()},
	}
	w.structFields("", root.body)
	return w.reads
}

func (w *walker) structFields(prefix string, body *ast.StructType) {
	if w.open[body] == maxSelfNesting {
		return
	}
	w.open[body]++
	defer func() { w.open[body]-- }()
	for _, field := range body.Fields.List {
		if embedded := w.embeddedStruct(field); embedded != nil {
			w.structFields(prefix, embedded)
			continue
		}
		for _, name := range jsonNames(field) {
			w.field(prefix+name, field.Type)
		}
	}
}

func (w *walker) field(path string, expr ast.Expr) {
	w.reads.Paths[path] = true
	w.contents(path, expr)
}

func (w *walker) element(path string, expr ast.Expr) {
	if body := w.structOf(expr); body != nil {
		w.structFields(path+".", body)
		return
	}
	w.field(path, expr)
}

func (w *walker) contents(path string, expr ast.Expr) {
	if array, ok := expr.(*ast.ArrayType); ok {
		w.element(path+"[]", array.Elt)
		return
	}
	if body := w.structOf(expr); body != nil {
		w.structFields(path+".", body)
		return
	}
	if keyed, ok := expr.(*ast.MapType); ok && !isRaw(keyed.Value) {
		w.reads.Maps = append(w.reads.Maps, path)
		w.field(path+".*", keyed.Value)
		return
	}
	if isRaw(expr) {
		w.reads.Opaque = append(w.reads.Opaque, path)
	}
}

func (w *walker) structOf(expr ast.Expr) *ast.StructType {
	if pointer, ok := expr.(*ast.StarExpr); ok {
		expr = pointer.X
	}
	switch e := expr.(type) {
	case *ast.StructType:
		return e
	case *ast.Ident:
		if declared := w.pkg.structs[cmp.Or(w.typeArgs[e.Name], e.Name)]; declared != nil {
			return declared.body
		}
	}
	return nil
}

func (w *walker) embeddedStruct(field *ast.Field) *ast.StructType {
	if len(field.Names) > 0 || jsonTagName(field) != "" {
		return nil
	}
	return w.structOf(field.Type)
}

func isRaw(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.MapType, *ast.InterfaceType:
		return true
	case *ast.Ident:
		return e.Name == "any"
	case *ast.SelectorExpr:
		return e.Sel.Name == "RawMessage"
	}
	return false
}

func jsonNames(field *ast.Field) []string {
	tagged := jsonTagName(field)
	if tagged == "-" {
		return nil
	}
	if len(field.Names) == 0 && tagged != "" {
		return []string{tagged}
	}
	var names []string
	for _, ident := range field.Names {
		if ident.IsExported() {
			names = append(names, cmp.Or(tagged, ident.Name))
		}
	}
	return names
}

func jsonTagName(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	name, _, _ := strings.Cut(reflect.StructTag(strings.Trim(field.Tag.Value, "`")).Get("json"), ",")
	return name
}
