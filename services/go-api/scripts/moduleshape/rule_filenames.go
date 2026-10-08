package main

import (
	"fmt"
	"go/ast"
	"path"
	"strings"
	"unicode"
)

func checkFileNames(files []sourceFile) []string {
	var lines []string
	for _, file := range files {
		names := exportedNames(file.file)
		stem := strings.TrimSuffix(path.Base(file.rel), ".go")
		if !file.test && len(names) > 0 && !namesStem(names, stem) {
			lines = append(lines, fmt.Sprintf("N3 %s matches no exported identifier", file.rel))
		}
	}
	return lines
}

func namesStem(names []string, stem string) bool {
	for _, name := range names {
		if snakeCase(name) == stem {
			return true
		}
	}
	return false
}

func exportedNames(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if typed.Recv == nil && typed.Name.IsExported() {
				names = append(names, typed.Name.Name)
			}
		case *ast.GenDecl:
			names = append(names, exportedTypes(typed)...)
		}
	}
	return names
}

func exportedTypes(gen *ast.GenDecl) []string {
	var names []string
	for _, spec := range gen.Specs {
		if typed, ok := spec.(*ast.TypeSpec); ok && typed.Name.IsExported() {
			names = append(names, typed.Name.Name)
		}
	}
	return names
}

func snakeCase(name string) string {
	runes := []rune(name)
	var out []rune
	for index, current := range runes {
		if index > 0 && unicode.IsUpper(current) && wordBreak(runes, index) {
			out = append(out, '_')
		}
		out = append(out, unicode.ToLower(current))
	}
	return string(out)
}

func wordBreak(runes []rune, index int) bool {
	previous := runes[index-1]
	if !unicode.IsUpper(previous) {
		return true
	}
	return index+1 < len(runes) && unicode.IsLower(runes[index+1])
}
