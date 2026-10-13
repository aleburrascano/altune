package main

import (
	"fmt"
	"path"
	"strings"
)

func checkContracts(files []sourceFile) []string {
	present := map[string]bool{}
	for _, file := range files {
		present[file.rel] = true
	}
	var lines []string
	for _, file := range files {
		dir, name := path.Split(file.rel)
		if file.test || dir != "internal/"+file.top+"/"+file.top+"test/" || !strings.HasSuffix(name, "_fake.go") {
			continue
		}
		stem := strings.TrimSuffix(name, "_fake.go")
		if !present[dir+stem+"_contract.go"] {
			lines = append(lines, fmt.Sprintf("G9 %s has no %s_contract.go", file.rel, stem))
		}
	}
	return lines
}
