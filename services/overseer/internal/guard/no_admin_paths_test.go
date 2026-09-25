package guard_test

import (
	"altune/overseer/internal/guard"
	"os"
	"path/filepath"
	"testing"
)

// Spine invariant: no go-api request path names "/admin" anywhere under
// internal/. Overseer's go-api surface moved to "/observe/*"; this fails the
// moment an "/admin" literal is reintroduced.
func TestNoAdminPaths(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve internal/ root: %v", err)
	}
	hits, err := guard.AdminPathLiterals(root)
	if err != nil {
		t.Fatalf("scan for /admin literals: %v", err)
	}
	for file, lits := range hits {
		t.Errorf("%s: found /admin path literal(s): %v", file, lits)
	}
}

// Proves the scan itself fires: a file with a reintroduced "/admin" literal is
// reported, not silently missed.
func TestNoAdminPaths_CatchesReintroducedLiteral(t *testing.T) {
	dir := t.TempDir()
	src := "package fixture\n\nconst reintroduced = \"/admin/health\"\n"
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	hits, err := guard.AdminPathLiterals(dir)
	if err != nil {
		t.Fatalf("scan fixture dir: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected the reintroduced /admin literal to be reported, found none")
	}
}
