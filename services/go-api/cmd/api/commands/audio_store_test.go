package commands

import (
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAudioStoreFromConfigKeepsCommandsInsideTheKeyPrefix(t *testing.T) {
	musicDir := t.TempDir()
	prodRef := "11111111-1111-1111-1111-111111111111/Artist/Album/Song.mp3"
	prodFile := filepath.Join(musicDir, prodRef)
	if err := os.MkdirAll(filepath.Dir(prodFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prodFile, []byte("prod audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "new.mp3")
	if err := os.WriteFile(source, []byte("new audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := storage.FromConfig(&config.Config{MusicDir: musicDir, AudioKeyPrefix: "staging/"}, slog.Default())
	if err != nil {
		t.Fatalf("storage.FromConfig: %v", err)
	}
	ctx := context.Background()

	if err := store.Delete(ctx, prodRef); err != nil {
		t.Fatalf("Delete outside the prefix = %v, want a nil no-op", err)
	}
	if _, err := os.Stat(prodFile); err != nil {
		t.Fatalf("Delete outside the prefix removed the object: %v", err)
	}
	if err := store.Store(ctx, source, "22222222-2222-2222-2222-222222222222/A/B/C.mp3"); err == nil {
		t.Fatal("Store outside the prefix succeeded, want an error")
	}
	if err := store.Store(ctx, source, "staging/22222222-2222-2222-2222-222222222222/A/B/C.mp3"); err != nil {
		t.Fatalf("Store inside the prefix = %v, want nil", err)
	}
}

func TestNewAudioStoreFromConfigLeavesAnUnprefixedStoreUnscoped(t *testing.T) {
	musicDir := t.TempDir()
	source := filepath.Join(t.TempDir(), "new.mp3")
	if err := os.WriteFile(source, []byte("new audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := storage.FromConfig(&config.Config{MusicDir: musicDir}, slog.Default())
	if err != nil {
		t.Fatalf("storage.FromConfig: %v", err)
	}
	if err := store.Store(context.Background(), source, "33333333-3333-3333-3333-333333333333/A/B/C.mp3"); err != nil {
		t.Fatalf("Store without a prefix = %v, want nil", err)
	}
}

func TestNewAudioStoreFromConfigForwardsAudioAgeListerForSweep(t *testing.T) {
	musicDir := t.TempDir()
	stagingRef := "staging/11111111-1111-1111-1111-111111111111/Artist/Album/Song.mp3"
	stagingFile := filepath.Join(musicDir, stagingRef)
	if err := os.MkdirAll(filepath.Dir(stagingFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagingFile, []byte("staging audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := storage.FromConfig(&config.Config{MusicDir: musicDir, AudioKeyPrefix: "staging/"}, slog.Default())
	if err != nil {
		t.Fatalf("storage.FromConfig: %v", err)
	}

	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		t.Fatal("expected the staging-scoped store built with AudioKeyPrefix=staging/ to satisfy ports.AudioAgeLister")
	}
	objects, err := lister.ListWithAge(context.Background(), "staging/")
	if err != nil {
		t.Fatalf("ListWithAge: %v", err)
	}
	if len(objects) != 1 || objects[0].AudioRef != stagingRef {
		t.Errorf("got %v, want a single entry for %q", objects, stagingRef)
	}
}

var rawSQLMethods = map[string]bool{"Query": true, "QueryRow": true, "Exec": true, "Begin": true}

func rawSQLCalls(t *testing.T, fset *token.FileSet, name string, src any) []string {
	t.Helper()
	file, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && rawSQLMethods[sel.Sel.Name] {
			hits = append(hits, fset.Position(call.Pos()).String()+" "+sel.Sel.Name)
		}
		return true
	})
	return hits
}

func TestRawSQLGuardPackageHasNoRawSQL(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		if hits := rawSQLCalls(t, token.NewFileSet(), path, nil); len(hits) > 0 {
			t.Errorf("raw SQL in cmd/api/commands, route it through a repository: %v", hits)
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files found; the guard checked nothing")
	}
	if _, err := os.Stat("audio_store.go"); err != nil {
		t.Fatalf("guard is not running in the commands package: %v", err)
	}
}

func TestRawSQLGuardFixtures(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"exec on pool", "package p\nfunc f() { pool.Exec(ctx, \"UPDATE tracks SET x = 1\") }", 1},
		{"query row", "package p\nfunc f() { _ = db.QueryRow(ctx, \"SELECT 1\") }", 1},
		{"query", "package p\nfunc f() { _, _ = tx.Query(ctx, \"SELECT 1\") }", 1},
		{"begin", "package p\nfunc f() { _, _ = pool.Begin(ctx) }", 1},
		{"look-alike method", "package p\nfunc f() { store.Exists(ctx, ref) }", 0},
		{"string literal", "package p\nvar s = \"Query\"\nfunc f() { println(\"Exec Begin\") }", 0},
		{"empty file", "package p\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawSQLCalls(t, token.NewFileSet(), "fixture.go", tc.src); len(got) != tc.want {
				t.Fatalf("got %d hits %v, want %d", len(got), got, tc.want)
			}
		})
	}
}
