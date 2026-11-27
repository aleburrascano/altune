package main

import (
	"altune/go-api/internal/shared/apierror"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const usage = "usage: go run ./scripts/genclientcodes [--check] <client apiErrorCodes.generated.ts>"

var plainKey = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func say(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func key(name string) string {
	if plainKey.MatchString(name) {
		return name
	}
	return "'" + name + "'"
}

func render(codes []apierror.Code) string {
	var b strings.Builder
	b.WriteString("export const API_ERROR_CODES = [\n")
	for _, c := range codes {
		say(&b, "  '%s',\n", c)
	}
	b.WriteString("] as const;\n\n")
	b.WriteString("export const API_ERROR_CODE_KINDS = {\n")
	for _, c := range codes {
		say(&b, "  %s: '%s',\n", key(c.String()), c.Kind())
	}
	b.WriteString("} as const satisfies Record<(typeof API_ERROR_CODES)[number], string>;\n")
	return b.String()
}

func run(args []string, stdout, stderr io.Writer) int {
	check := false
	var paths []string
	for _, arg := range args {
		if arg == "--check" {
			check = true
			continue
		}
		paths = append(paths, arg)
	}
	if len(paths) != 1 {
		say(stderr, "%s\n", usage)
		return 2
	}
	want := render(apierror.Catalog())
	path := paths[0]
	if !check {
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			say(stderr, "genclientcodes: %v\n", err)
			return 1
		}
		say(stdout, "wrote %s\n", path)
		return 0
	}
	have, err := os.ReadFile(path)
	if err != nil {
		say(stderr, "genclientcodes: %v\n", err)
		return 1
	}
	if !bytes.Equal(have, []byte(want)) {
		say(stderr, "%s is stale: run `cd services/go-api && go run ./scripts/genclientcodes ../../%s` and commit it\n", path, relativeToRepo(path))
		return 1
	}
	return 0
}

func relativeToRepo(path string) string {
	const marker = "client/"
	if i := strings.Index(path, marker); i >= 0 {
		return path[i:]
	}
	return path
}
