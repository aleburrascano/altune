package main

import "fmt"

func checkPlainLogs(files []sourceFile) []string {
	var lines []string
	for _, file := range files {
		if file.test {
			continue
		}
		for _, site := range selectorCalls(file, "slog", "Info", "Warn", "Error", "Debug") {
			lines = append(lines, fmt.Sprintf("G7 %s:%s %s without context", file.rel, site.fn, site.name))
		}
	}
	return lines
}
