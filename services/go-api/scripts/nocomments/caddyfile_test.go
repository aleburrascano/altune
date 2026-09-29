package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type caddyCut struct {
	text string
	line int
}

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker unavailable")
	}
}

func repoFile(t *testing.T, rel string) []byte {
	t.Helper()
	rootOut, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("git rev-parse --show-toplevel unavailable")
	}
	src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(rootOut)), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return src
}

var caddyfileCommentRows = []struct {
	name string
	src  string
	want []caddyCut
}{
	{"rule 1 trailing comment takes the space before it", ":80 {\n\trespond ok # c\n}\n", []caddyCut{{" # c", 2}}},
	{"rule 1 comment-only line takes the whole line", ":80 {\n\t# c\n\trespond ok\n}\n", []caddyCut{{"\t# c\n", 2}}},
	{"rule 1 hash inside a token is literal", ":80 {\n\trespond a#b\n}\n", nil},
	{"rule 2 escaped quote stays inside the quotes", ":80 {\n\trespond \"a\\\"b # c\" 200\n}\n", nil},
	{"rule 2 quoted token spans lines", ":80 {\n\trespond \"a\n# b\" 200 # c\n}\n", []caddyCut{{" # c", 3}}},
	{"rule 2 CSP with a hash in a sha", ":80 {\n\theader Content-Security-Policy \"script-src 'sha256-x#y' 'self'\" # c\n}\n", []caddyCut{{" # c", 2}}},
	{"rule 3 backtick keeps hash", ":80 {\n\trespond `a # b` 200\n}\n", nil},
	{"rule 3 backslash is literal in backticks", ":80 {\n\trespond `a\\` 200 # c\n}\n", []caddyCut{{" # c", 2}}},
	{"rule 4 heredoc marker with a dash", ":80 {\n\trespond <<MY-MARKER\n\t# not a comment\n\tMY-MARKER 200\n}\n", nil},
	{"rule 4 marker followed by a space is an ordinary token and keeps that space", ":80 {\n\trespond <<x # c\n}\n", []caddyCut{{"# c", 2}}},
	{"rule 4 escaped heredoc opener", ":80 {\n\trespond \\<<END\n\t# c\n}\n", []caddyCut{{"\t# c\n", 3}}},
	{"rule 5 heredoc closes mid-line on the marker", ":80 {\n\trespond <<END\n\tfooEND 200 # x\n}\n", []caddyCut{{" # x", 3}}},
	{"rule 6 escaped hash starts a comment and keeps the backslash", ":80 {\n\trespond ok \\#x\n\t200\n}\n", []caddyCut{{"#x", 2}}},
	{"span keeps the newline after a joined line", ":80 {\n\trespond ok \\\n\t# x\n}\n", []caddyCut{{"\t# x", 3}}},
	{"CRLF trailing comment stops before the CR", ":80 {\r\n\trespond ok # x\r\n}\r\n", []caddyCut{{" # x", 2}}},
	{"CRLF comment-only line takes its CRLF", ":80 {\r\n\t# c\r\n\trespond ok\r\n}\r\n", []caddyCut{{"\t# c\r\n", 2}}},
	{"comment at EOF without newline", ":80 {\n}\n# x", []caddyCut{{"# x", 3}}},
	{"byte order mark is skipped", "\xEF\xBB\xBF# c\n:80 {\n}\n", []caddyCut{{"# c\n", 1}}},
	{"line numbers count heredoc and quote lines", ":80 {\n\trespond <<E\n\ta\n\tE 200\n\trespond \"a\nb\" 200 # c\n}\n", []caddyCut{{" # c", 6}}},
}

func TestCaddyfileCommentsRows(t *testing.T) {
	for _, tc := range caddyfileCommentRows {
		t.Run(tc.name, func(t *testing.T) {
			spans, err := caddyfileComments([]byte(tc.src))
			if err != nil {
				t.Fatalf("caddyfileComments: %v", err)
			}
			if len(spans) != len(tc.want) {
				t.Fatalf("got %d spans %v, want %d", len(spans), spans, len(tc.want))
			}
			for i, s := range spans {
				if got := tc.src[s.start:s.end]; got != tc.want[i].text || s.line != tc.want[i].line {
					t.Errorf("span %d = %q line %d, want %q line %d", i, got, s.line, tc.want[i].text, tc.want[i].line)
				}
			}
		})
	}
}

func TestCaddyfileCommentsRowsAdaptToTheSameConfigWhenStripped(t *testing.T) {
	requireDocker(t)
	for _, tc := range caddyfileCommentRows {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.src)
			spans, err := caddyfileComments(src)
			if err != nil {
				t.Fatalf("caddyfileComments: %v", err)
			}
			if err := caddyfileKind.same(src, deleteSpans(src, spans)); err != nil {
				t.Fatalf("same: %v", err)
			}
		})
	}
}

func TestCaddyfileCommentsRejectsWhatCaddyRejectsOrCannotBeReadHonestly(t *testing.T) {
	cases := []struct{ name, src string }{
		{"rule 4 triple angle bracket", ":80 {\n\trespond <<<X\n\thi\n}\n"},
		{"rule 4 empty marker", ":80 {\n\trespond <<\n\thi\n}\n"},
		{"rule 4 bad marker character", ":80 {\n\trespond <<EN#D\n\thi\n\tEN#D\n}\n"},
		{"rule 5 unclosed heredoc", ":80 {\n\trespond <<END\n\thi\n}\n"},
		{"rule 7 unterminated quote", ":80 {\n\trespond \"abc # x\n}\n"},
		{"rule 7 unterminated backtick", ":80 {\n\trespond `abc # x\n}\n"},
		{"rule 7 lone quote at EOF", ":80 {\n}\n\""},
		{"backslash in a comment joins the next line", ":80 {\n\t# a \\\n\trespond ok\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if spans, err := caddyfileComments([]byte(tc.src)); err == nil {
				t.Fatalf("want error, got spans %v", spans)
			}
		})
	}
}

func TestCaddyfileMatch(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"services/go-api/deploy/Caddyfile", true},
		{"Caddyfile", true},
		{"services/go-api/deploy/caddy/staging-upstream.conf", true},
		{"caddy/upstream.conf", true},
		{"services/go-api/deploy/other/x.conf", false},
		{"services/go-api/deploy/caddy/notes.md", false},
		{"Caddyfile.bak", false},
	}
	for _, tc := range cases {
		if got := caddyfileKind.match(tc.path, nil); got != tc.want {
			t.Errorf("match(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestCaddyAdaptImageMatchesComposeProdPin(t *testing.T) {
	compose := string(repoFile(t, "services/go-api/deploy/compose.prod.yml"))
	pin := regexp.MustCompile(`(?m)^  caddy:\n\s+image: (\S+)`).FindStringSubmatch(compose)
	if pin == nil {
		t.Fatal("caddy service image not found in compose.prod.yml")
	}
	if pin[1] != caddyAdaptImage {
		t.Errorf("compose.prod.yml pins %q, caddyAdaptImage is %q", pin[1], caddyAdaptImage)
	}
}

func TestCaddyfileSameRejectsAChangedConfig(t *testing.T) {
	requireDocker(t)
	before := []byte(":80 {\n\trespond ok\n}\n")
	after := []byte(":80 {\n\trespond no\n}\n")
	if err := caddyfileKind.same(before, after); err == nil {
		t.Fatal("same: want error for a changed body, got nil")
	}
}

func TestCaddyfileSameWrapsBareDirectiveFragments(t *testing.T) {
	requireDocker(t)
	before := []byte("reverse_proxy x:8000 # c\n")
	after := []byte("reverse_proxy x:8000\n")
	if err := caddyfileKind.same(before, after); err != nil {
		t.Fatalf("same: %v", err)
	}
}

func TestWriteCaddyStubRefusesPathsOutsideTheStubRoot(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"/", "/..", "relative.conf"} {
		if err := writeCaddyStub(root, path); err == nil {
			t.Errorf("writeCaddyStub(%q): want error, got nil", path)
		}
	}
}

func TestTrackedCaddyConfigsStripToZeroCommentsAndAdaptUnchanged(t *testing.T) {
	requireDocker(t)
	for _, rel := range []string{
		"services/go-api/deploy/Caddyfile",
		"services/go-api/deploy/caddy/staging-upstream.conf",
	} {
		t.Run(rel, func(t *testing.T) {
			src := repoFile(t, rel)
			out, count, err := strip(src, caddyfileKind)
			if err != nil {
				t.Fatalf("strip: %v", err)
			}
			if count == 0 {
				t.Fatal("strip removed 0 comments, want the tracked file's comments")
			}
			remaining, err := caddyfileComments(out)
			if err != nil {
				t.Fatalf("caddyfileComments stripped: %v", err)
			}
			if len(remaining) != 0 {
				t.Fatalf("stripped output still has %d comments", len(remaining))
			}
		})
	}
}
