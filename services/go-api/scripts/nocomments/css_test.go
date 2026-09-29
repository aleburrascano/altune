package main

import "testing"

func TestCssMatchByExtension(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"services/overseer/web/src/styles.css", true},
		{"a.css", true},
		{"a.CSS", false},
		{"a.scss", false},
		{"a.js", false},
	}
	for _, tc := range cases {
		if got := cssKind.match(tc.path, nil); got != tc.want {
			t.Errorf("cssKind.match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func stripCSS(t *testing.T, src string) string {
	t.Helper()
	out, _, err := strip([]byte(src), cssKind)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	return string(out)
}

func TestCssStripDeletesHeaderBlockAndTrailingCommentAndKeepsStringsAndUrls(t *testing.T) {
	src := "/* header\n   block */\n\na {\n  color: red; /* x */\n  content: \"/* not */\";\n  background: url(a/*b);\n}\n"
	want := "\na {\n  color: red;\n  content: \"/* not */\";\n  background: url(a/*b);\n}\n"
	if got := stripCSS(t, src); got != want {
		t.Fatalf("strip =\n%q\nwant\n%q", got, want)
	}
}

func TestCssStripDeletesALineHoldingOnlyCommentsWholeWithNoStrayBlankLine(t *testing.T) {
	src := "a {}\n  /* a */ /* b */\nb {}\n"
	if got, want := stripCSS(t, src), "a {}\nb {}\n"; got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestCssStripRunsAnUnterminatedCommentToEndOfFile(t *testing.T) {
	src := "a {}\n/* never closed\nb {}\n"
	if got, want := stripCSS(t, src), "a {}\n"; got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestCssCommentFreeTokensEqualRejectsAChangedValue(t *testing.T) {
	if err := cssCommentFreeTokensEqual([]byte("a { color: red; }"), []byte("a { color: blue; }")); err == nil {
		t.Fatal("want error for a changed token, got nil")
	}
}

func TestCssCommentFreeTokensEqualIgnoresCommentsAndWhitespace(t *testing.T) {
	if err := cssCommentFreeTokensEqual([]byte("a {/* c */ color: red; }"), []byte("a { color: red;}")); err != nil {
		t.Fatalf("want equal, got %v", err)
	}
}

func TestCssCommentsRepoWideTrackedFiles(t *testing.T) {
	assertStripsToACleanEquivalent(t, cssKind, trackedFilesMatching(t, cssKind))
}
