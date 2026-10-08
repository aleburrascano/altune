package main

import (
	"fmt"
	"go/ast"
	"regexp"
	"strings"
)

const httputilPrefix = "internal/shared/httputil/"

var handlerConstructor = regexp.MustCompile(`^New[A-Z]\w*Handler$`)

func checkHTTPError(files []sourceFile) []string {
	var lines []string
	for _, file := range files {
		if file.test || strings.HasPrefix(file.rel, httputilPrefix) {
			continue
		}
		for _, site := range selectorCalls(file, "http", "Error") {
			lines = append(lines, fmt.Sprintf("G4 %s:%s %s outside shared/httputil", file.rel, site.fn, site.name))
		}
	}
	return lines
}

func checkCodedErrors(files []sourceFile) []string {
	var lines []string
	for _, file := range files {
		if file.test || strings.Contains(file.rel, "/domain/") {
			continue
		}
		for _, decl := range file.file.Decls {
			if owner := codedErrorOwner(decl); owner != "" {
				lines = append(lines, fmt.Sprintf("G4 %s %s coded error declared outside domain", file.rel, owner))
			}
		}
	}
	return lines
}

func codedErrorOwner(decl ast.Decl) string {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Name.Name != "ErrorCode" {
		return ""
	}
	return receiverName(fn)
}

func checkHandlerNames(files []sourceFile) []string {
	var lines []string
	for _, file := range files {
		if file.test || !strings.HasSuffix(filepathDir(file.rel), "/handler") {
			continue
		}
		for _, decl := range file.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "New") && !handlerConstructor.MatchString(fn.Name.Name) {
				lines = append(lines, fmt.Sprintf("G5 %s:%s constructor is not New<Resource>Handler", file.rel, fn.Name.Name))
			}
		}
	}
	return lines
}

func filepathDir(rel string) string {
	index := strings.LastIndex(rel, "/")
	if index < 0 {
		return ""
	}
	return rel[:index]
}
