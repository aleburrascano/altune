package main

import "testing"

func TestJsMatchExtensionsAndExcludedTrees(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"commitlint.config.js", true},
		{"scripts/check-cycles.mjs", true},
		{"a.cjs", true},
		{"../../dangerfile.js", true},
		{"a.ts", false},
		{"a.jsx", false},
		{"apps/mobile/babel.config.js", false},
		{"/repo/services/overseer/web/vite.config.js", false},
		{"services/overseer/other.js", true},
		{"tools/node_modules/x/index.js", false},
	}
	for _, tc := range cases {
		if got := jsKind.match(tc.path, nil); got != tc.want {
			t.Errorf("jsKind.match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func stripJS(t *testing.T, src string) string {
	t.Helper()
	out, _, err := strip([]byte(src), jsKind)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	return string(out)
}

func TestJsStripDeletesCommentsAndKeepsLookalikesAndShebang(t *testing.T) {
	src := "#!/usr/bin/env node\n" +
		"// x\n" +
		"/**\n * doc\n */\n" +
		"const a = '//'; /* y */\n" +
		"const b = /\\/\\//;\n" +
		"const c = `/* */`;\n"
	want := "#!/usr/bin/env node\n" +
		"const a = '//';\n" +
		"const b = /\\/\\//;\n" +
		"const c = `/* */`;\n"
	if got := stripJS(t, src); got != want {
		t.Fatalf("strip =\n%q\nwant\n%q", got, want)
	}
}

func TestJsStripKeepsARegexWithAnEscapedSlashAfterABlockOrClauseEnd(t *testing.T) {
	cases := []struct{ name, src string }{
		{"function declaration", "function f(){}\n/a\\/b/.test(x); // c\n"},
		{"else block", "if (x) {} else {}\n/a\\/b/.test(x); // c\n"},
		{"try finally", "try {} finally {}\n/a\\/b/.test(x); // c\n"},
		{"class body", "class A { m(){} }\n/a\\/b/.test(x); // c\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.src[:len(tc.src)-len(" // c\n")] + "\n"
			if got := stripJS(t, tc.src); got != want {
				t.Fatalf("strip = %q, want %q", got, want)
			}
		})
	}
}

func TestJsStripKeepsRealDivisionAndDeletesOnlyTheTrailingComment(t *testing.T) {
	src := "const o = {a: 1}\nconst q = o.a / 2 / 3; // c\n"
	want := "const o = {a: 1}\nconst q = o.a / 2 / 3;\n"
	if got := stripJS(t, src); got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestJsStripKeepsDivisionInsideATemplateSubstitution(t *testing.T) {
	src := "const s = `${ {a: 1}.a / 2 } //no`; // yes\n"
	want := "const s = `${ {a: 1}.a / 2 } //no`;\n"
	if got := stripJS(t, src); got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestJsCommentsFailsClosedOnAnArrowBodyFollowedByARegexLine(t *testing.T) {
	src := []byte("() => { }\n/a\\/b/.test(x)\n")
	spans, err := jsComments(src)
	if err == nil {
		t.Fatalf("jsComments = %v, want a parse error", spans)
	}
	if _, _, err := strip(src, jsKind); err == nil {
		t.Fatal("strip: want the parse error, got nil")
	}
}

func TestJsStripReplacesAMultiLineCommentBetweenCodeWithOneNewline(t *testing.T) {
	src := "a(); /* one\ntwo */ b();\n"
	if got, want := stripJS(t, src), "a();\nb();\n"; got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestJsStripDeletesALineHoldingOnlyCommentsWhole(t *testing.T) {
	src := "a();\n  /* a */ /* b */ // c\nb();\n"
	if got, want := stripJS(t, src), "a();\nb();\n"; got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestJsStripKeepsCRLFWhenRemovingAMultiLineCommentBetweenCode(t *testing.T) {
	src := "a(); /* one\r\ntwo */ b();\r\n"
	if got, want := stripJS(t, src), "a();\r\nb();\r\n"; got != want {
		t.Fatalf("strip = %q, want %q", got, want)
	}
}

func TestJsCommentFreeTokensEqualRejectsAChangedToken(t *testing.T) {
	if err := jsCommentFreeTokensEqual([]byte("a = 1;\n"), []byte("a = 2;\n")); err == nil {
		t.Fatal("want error for a changed token, got nil")
	}
}

func TestJsCommentFreeTokensEqualRejectsALostLineTerminator(t *testing.T) {
	if err := jsCommentFreeTokensEqual([]byte("a\nb\n"), []byte("a b\n")); err == nil {
		t.Fatal("want error when a newline between tokens disappears, got nil")
	}
}

func TestJsCommentsFailsOnAnUnterminatedBlockComment(t *testing.T) {
	if _, err := jsComments([]byte("a(); /* open\n")); err == nil {
		t.Fatal("want a lex error, got nil")
	}
}

func TestJsCommentsRepoWideTrackedFiles(t *testing.T) {
	assertStripsToACleanEquivalent(t, jsKind, trackedFilesMatching(t, jsKind))
}
