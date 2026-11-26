package guard_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNoHTMLTemplateInCoreOrBuckets(t *testing.T) {
	pkgs := []string{"altune/overseer/internal/core"}
	pkgs = append(pkgs, bucketPackages(t)...)

	for _, pkg := range pkgs {
		for _, dep := range deps(t, pkg) {
			if dep == "html/template" {
				t.Errorf("%s imports html/template; the contract is JSON-only (React escapes on render)", pkg)
			}
		}
	}
}

func bucketPackages(t *testing.T) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "altune/overseer/internal/buckets/...").Output()
	if err != nil {
		t.Fatalf("go list buckets: %v", err)
	}
	pkgs := strings.Fields(string(out))
	if len(pkgs) == 0 {
		t.Fatal("go list returned no bucket packages")
	}
	return pkgs
}
