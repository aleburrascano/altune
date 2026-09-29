package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkupMatchByExtensionOnly(t *testing.T) {
	cases := []struct {
		path     string
		markdown bool
		html     bool
	}{
		{"README.md", true, false},
		{"docs/a/plan.md", true, false},
		{"web/index.html", false, true},
		{"page.htm", false, false},
		{"notes.txt", false, false},
	}
	for _, tc := range cases {
		if got := markdownKind.match(tc.path, []byte("<!doctype html>")); got != tc.markdown {
			t.Errorf("markdownKind.match(%q) = %v, want %v", tc.path, got, tc.markdown)
		}
		if got := htmlKind.match(tc.path, []byte("# heading")); got != tc.html {
			t.Errorf("htmlKind.match(%q) = %v, want %v", tc.path, got, tc.html)
		}
	}
}

func stripFixture(t *testing.T, name, content string) (string, int, string) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, name, content)
	var stdout strings.Builder
	code := run([]string{"strip", filepath.Join(dir, name)}, &stdout)
	after, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(after), code, stdout.String()
}

func TestStripMarkdownDeletesBlockInlineAndNestedCommentsAndKeepsCode(t *testing.T) {
	src := "# Title\n\n<!-- block -->\n\nsome text <!-- inline --> more\n\n<div>\n<!-- nested -->\n<p>x</p>\n</div>\n\n`<!-- code -->`\n\n```\n<!-- fenced -->\n```\n"
	want := "# Title\n\n\nsome text  more\n\n<div>\n<p>x</p>\n</div>\n\n`<!-- code -->`\n\n```\n<!-- fenced -->\n```\n"

	got, code, out := stripFixture(t, "doc.md", src)

	if code != 0 {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	if got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestStripMarkdownOpeningWithDivKeepsCodeSpan(t *testing.T) {
	src := "<div>\nhello\n</div>\n\nuse `<!-- code -->` here\n\n<!-- gone -->\n"
	want := "<div>\nhello\n</div>\n\nuse `<!-- code -->` here\n\n"

	got, code, out := stripFixture(t, "doc.md", src)

	if code != 0 {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	if got != want {
		t.Fatalf("stripped = %q, want %q", got, want)
	}
}

func TestCheckHTMLFragmentCountsLeadingAndNestedCommentsOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "frag.html", "<!-- lead -->\n<div>\n<!-- nested -->\n<p>x</p>\n</div>\n")
	writeFile(t, dir, "raw.html", "<div><![CDATA[ x ]]></div>\n<script>var a = \"<!-- not a comment -->\";</script>\n")

	var frag strings.Builder
	if code := run([]string{"check", filepath.Join(dir, "frag.html")}, &frag); code != 1 {
		t.Fatalf("frag exit = %d, want 1; output:\n%s", code, frag.String())
	}
	if !strings.Contains(frag.String(), "frag.html:1") || !strings.Contains(frag.String(), "frag.html:3") {
		t.Fatalf("frag output should name lines 1 and 3:\n%s", frag.String())
	}
	if n := strings.Count(frag.String(), "frag.html:"); n != 2 {
		t.Fatalf("frag reported %d comments, want 2:\n%s", n, frag.String())
	}

	var raw strings.Builder
	if code := run([]string{"check", filepath.Join(dir, "raw.html")}, &raw); code != 0 {
		t.Fatalf("raw exit = %d, want 0; output:\n%s", code, raw.String())
	}
}

func TestStripMarkdownRefusesATrailingTextCommentAndLeavesTheFile(t *testing.T) {
	src := "before\n\n<!-- x --> trailing text\n\nafter\n"

	got, code, out := stripFixture(t, "doc.md", src)

	if code != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", code, out)
	}
	if got != src {
		t.Fatalf("file changed to %q", got)
	}
}

func TestMarkdownSameRejectsAnOversizedDeletion(t *testing.T) {
	before := []byte("before\n\n<!-- x --> trailing text\n\nafter\n")
	after := []byte("before\n\nafter\n")

	if err := markdownKind.same(before, after); err == nil {
		t.Fatal("markdownKind.same accepted deleting the whole line, want an error")
	}
}

func TestMarkdownSameAcceptsAnInlineStrip(t *testing.T) {
	if err := markdownKind.same([]byte("a <!-- y --> b\n"), []byte("a  b\n")); err != nil {
		t.Fatalf("markdownKind.same: %v", err)
	}
}

func TestHTMLSameRejectsByteLoss(t *testing.T) {
	before := []byte("<p>a</p>\n<!-- c -->\n<p>b</p>\n")
	after := []byte("<p>a</p>\n<p>\n")

	if err := htmlKind.same(before, after); err == nil {
		t.Fatal("htmlKind.same accepted lost content, want an error")
	}
}

func TestMarkupCommentsRepoWideTrackedFiles(t *testing.T) {
	repoWideCheck(t, markdownKind)
	repoWideCheck(t, htmlKind)
}
