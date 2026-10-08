package main

import (
	"fmt"
	"go/ast"
	"strings"
)

func checkFakes(files []sourceFile) []string {
	declared := map[string][]string{}
	for _, file := range files {
		if !file.test || strings.Contains(file.rel, "/"+file.top+"test/") {
			continue
		}
		for _, name := range fakeTypes(file.file) {
			key := file.top + " " + name
			declared[key] = append(declared[key], file.rel)
		}
	}
	return repeatedFakes(declared)
}

func repeatedFakes(declared map[string][]string) []string {
	var lines []string
	for key, paths := range declared {
		_, name, _ := strings.Cut(key, " ")
		for _, path := range paths {
			if len(paths) > 1 {
				lines = append(lines, fmt.Sprintf("G6 %s %s declared in several test files", path, name))
			}
		}
	}
	return lines
}

func fakeTypes(file *ast.File) []string {
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			if typed, ok := spec.(*ast.TypeSpec); ok && isFakeName(typed.Name.Name) {
				names = append(names, typed.Name.Name)
			}
		}
	}
	return names
}

func isFakeName(name string) bool {
	return strings.HasPrefix(name, "fake") || strings.HasPrefix(name, "stub")
}
