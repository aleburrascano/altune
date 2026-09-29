package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sqlKeepers = "SELECT '-- not a comment', 'O''Brien', \"col--name\", \"a\"\"b\", E'it\\'s -- here';\n" +
	"CREATE FUNCTION f() RETURNS int AS $fn$ SELECT /* in */ 1 -- in2\n$fn$ LANGUAGE sql;\n"

const sqlFixture = "-- header one\n-- header two\n" + sqlKeepers +
	"SELECT 1; -- trailing\n" +
	"/* multi\n   /* nested */\n   line */\n" +
	"SELECT 2;\n"

func TestSQLMatchByExtension(t *testing.T) {
	for path, want := range map[string]bool{"a/001_x.sql": true, "x.SQL": false, "x.sh": false} {
		if got := sqlKind.match(path, nil); got != want {
			t.Errorf("sqlKind.match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSQLStripDeletesCommentsAndKeepsQuotedLookalikes(t *testing.T) {
	out, n, err := strip([]byte(sqlFixture), sqlKind)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	want := sqlKeepers + "SELECT 1;\nSELECT 2;\n"
	if string(out) != want || n != 4 {
		t.Errorf("strip = %d comments, %q; want 4, %q", n, out, want)
	}
}

func TestSQLCommentsFindsTopLevelCommentBetweenDollarBodies(t *testing.T) {
	spans, err := sqlComments([]byte("SELECT $$can't$$;\n-- real comment\nSELECT $$won't$$;\n"))
	if err != nil {
		t.Fatalf("sqlComments: %v", err)
	}
	if len(spans) != 1 || spans[0].line != 2 {
		t.Errorf("spans = %+v, want exactly one on line 2", spans)
	}
}

func TestSQLCommentsErrorsOnUnterminatedConstructs(t *testing.T) {
	for _, src := range []string{"SELECT 'abc\n-- c\n", "SELECT \"abc\n", "SELECT /* open\n", "SELECT $x$ open\n"} {
		if _, err := sqlComments([]byte(src)); err == nil {
			t.Errorf("sqlComments(%q) succeeded, want an error", src)
		}
	}
}

func TestSQLSameRejectsDifferentStatements(t *testing.T) {
	if err := sqlKind.same([]byte("SELECT 1;"), []byte("SELECT 2;")); err == nil {
		t.Error("same accepted SELECT 1 vs SELECT 2")
	}
	if err := sqlKind.same([]byte("SELECT 1; -- x"), []byte("SELECT 1;")); err != nil {
		t.Errorf("same rejected comment-only difference: %v", err)
	}
}

func TestSQLStripKeepsEveryTrackedSQLFileEquivalentAndCommentFree(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := exec.Command("git", "-C", root, "ls-files", "*.sql").Output()
	if err != nil {
		t.Fatal(err)
	}
	files := strings.Fields(string(listed))
	if len(files) == 0 {
		t.Fatal("no tracked .sql files found")
	}
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		out, _, err := strip(src, sqlKind)
		if err != nil {
			t.Errorf("%s: strip: %v", f, err)
			continue
		}
		if err := sqlKind.same(src, out); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		tmp := filepath.Join(t.TempDir(), "out.sql")
		if err := os.WriteFile(tmp, out, 0o600); err != nil {
			t.Fatal(err)
		}
		var sink strings.Builder
		hits, err := reportTarget(target{display: f, read: tmp, kind: sqlKind}, &sink)
		if err != nil || hits != 0 {
			t.Errorf("%s: stripped output still reports %d comments (err %v)", f, hits, err)
		}
	}
}
