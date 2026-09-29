package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	caddyAdaptImage   = "caddy:2-alpine"
	caddyAdaptTimeout = 120 * time.Second
	caddyMaxStubs     = 32
	caddyUTF8BOM      = "\xEF\xBB\xBF"
	noJoiningNewline  = -2
)

var (
	caddyHeredocMarker = regexp.MustCompile("^[A-Za-z0-9_-]+$")
	caddyMissingImport = regexp.MustCompile(`File to import not found: (.+), at `)
)

func init() { register(caddyfileKind) }

var caddyfileKind = kind{
	name:     "caddyfile",
	match:    matchCaddyfile,
	comments: caddyfileComments,
	same:     func(before, after []byte) error { return caddyAdaptSame(caddyAdaptTimeout, before, after) },
}

func matchCaddyfile(path string, _ []byte) bool {
	if filepath.Base(path) == "Caddyfile" {
		return true
	}
	return strings.HasSuffix(path, ".conf") && filepath.Base(filepath.Dir(path)) == "caddy"
}

type caddyToken struct {
	val            []rune
	marker         string
	openLine       int
	quoted         bool
	backtick       bool
	inHeredoc      bool
	heredocEscaped bool
	escaped        bool
	comment        bool
	commentEscaped bool
	commentStart   int
}

func (t *caddyToken) isHeredocOpener() bool {
	return !t.quoted && !t.backtick && !t.inHeredoc && !t.heredocEscaped &&
		len(t.val) > 1 && t.val[0] == '<' && t.val[1] == '<'
}

type caddyScanState struct {
	src           []byte
	i, line       int
	lineStart     int
	joinedNewline int
	openerSpace   int
	tok           caddyToken
	spans         []span
}

func caddyfileComments(src []byte) ([]span, error) {
	st := &caddyScanState{src: src, line: 1, joinedNewline: noJoiningNewline, openerSpace: noJoiningNewline}
	if bytes.HasPrefix(src, []byte(caddyUTF8BOM)) {
		st.i = len(caddyUTF8BOM)
		st.lineStart = st.i
	}
	for st.i < len(st.src) {
		if err := st.step(); err != nil {
			return nil, err
		}
	}
	if err := st.finish(); err != nil {
		return nil, err
	}
	return st.spans, nil
}

func (st *caddyScanState) step() error {
	ch, width := utf8.DecodeRune(st.src[st.i:])
	pos := st.i
	st.i += width
	if err := st.consume(ch, pos); err != nil {
		return err
	}
	if ch == '\n' {
		st.line++
		st.lineStart = st.i
	}
	return nil
}

func (st *caddyScanState) consume(ch rune, pos int) error {
	t := &st.tok
	switch {
	case t.isHeredocOpener():
		return st.heredocOpenerChar(ch)
	case t.inHeredoc:
		st.heredocChar(ch)
	case !t.escaped && !t.backtick && ch == '\\':
		t.escaped = true
	case t.quoted || t.backtick:
		st.quotedChar(ch)
	case unicode.IsSpace(ch):
		return st.spaceChar(ch, pos)
	default:
		st.plainChar(ch, pos)
	}
	return nil
}

func (st *caddyScanState) heredocOpenerChar(ch rune) error {
	t := &st.tok
	switch ch {
	case ' ':
		st.openerSpace = st.i - 1
		st.tok = caddyToken{}
	case '\r':
	case '\n':
		return st.openHeredoc()
	default:
		t.val = append(t.val, ch)
	}
	return nil
}

func (st *caddyScanState) openHeredoc() error {
	t := &st.tok
	if len(t.val) == 2 {
		return fmt.Errorf("line %d: empty heredoc marker", st.line)
	}
	if string(t.val[:3]) == "<<<" {
		return fmt.Errorf("line %d: too many '<' for heredoc", st.line)
	}
	marker := string(t.val[2:])
	if !caddyHeredocMarker.MatchString(marker) {
		return fmt.Errorf("line %d: heredoc marker %q must contain only alphanumeric characters, dashes and underscores", st.line, marker)
	}
	t.marker = marker
	t.inHeredoc = true
	t.openLine = st.line
	t.val = nil
	return nil
}

func (st *caddyScanState) heredocChar(ch rune) {
	t := &st.tok
	t.val = append(t.val, ch)
	if strings.HasSuffix(string(t.val), t.marker) {
		st.tok = caddyToken{}
	}
}

func (st *caddyScanState) quotedChar(ch rune) {
	t := &st.tok
	if t.quoted && t.escaped {
		t.escaped = false
		return
	}
	if (t.quoted && ch == '"') || (t.backtick && ch == '`') {
		st.tok = caddyToken{}
	}
}

func (st *caddyScanState) spaceChar(ch rune, pos int) error {
	t := &st.tok
	if ch == '\r' {
		return nil
	}
	if ch == '\n' {
		if t.escaped {
			st.joinedNewline = pos
			t.escaped = false
		}
		if t.comment {
			if err := st.endComment(pos); err != nil {
				return err
			}
			t.comment = false
		}
	}
	if len(t.val) > 0 {
		st.tok = caddyToken{}
	}
	return nil
}

func (st *caddyScanState) plainChar(ch rune, pos int) {
	t := &st.tok
	if ch == '#' && len(t.val) == 0 && !t.comment {
		t.comment = true
		t.commentStart = pos
		t.commentEscaped = t.escaped
	}
	if t.comment || st.openQuote(ch) {
		return
	}
	if t.escaped {
		if ch == '<' {
			t.heredocEscaped = true
		} else {
			t.val = append(t.val, '\\')
		}
		t.escaped = false
	}
	t.val = append(t.val, ch)
}

func (st *caddyScanState) openQuote(ch rune) bool {
	t := &st.tok
	if len(t.val) > 0 || (ch != '"' && ch != '`') {
		return false
	}
	t.quoted = ch == '"'
	t.backtick = ch == '`'
	t.openLine = st.line
	return true
}

func (st *caddyScanState) finish() error {
	t := &st.tok
	if t.comment {
		if err := st.endComment(len(st.src)); err != nil {
			return err
		}
	}
	if t.quoted || t.backtick {
		return fmt.Errorf("unterminated quoted token starting at line %d", t.openLine)
	}
	if t.inHeredoc {
		return fmt.Errorf("unterminated heredoc <<%s starting at line %d", t.marker, t.openLine)
	}
	return nil
}

func (st *caddyScanState) endComment(end int) error {
	t := &st.tok
	if !t.commentEscaped && bytes.IndexByte(st.src[t.commentStart:end], '\\') >= 0 {
		return fmt.Errorf("line %d: a backslash in a comment joins the next line, so the comment can't be removed safely", st.line)
	}
	joinedBefore := st.joinedNewline == st.lineStart-1
	st.spans = append(st.spans, caddyCommentSpan(st.src, st.lineStart, t.commentStart, end, st.line, st.openerSpace+1, joinedBefore))
	return nil
}

func caddyCommentSpan(src []byte, lineStart, hashPos, end, line, keepFrom int, joinedBefore bool) span {
	endBeforeCR := end
	if endBeforeCR > hashPos && src[endBeforeCR-1] == '\r' {
		endBeforeCR--
	}
	if !isBlank(src[lineStart:hashPos]) {
		return span{max(trimTrailingSpace(src, lineStart, hashPos), keepFrom), endBeforeCR, line}
	}
	if joinedBefore {
		return span{lineStart, endBeforeCR, line}
	}
	consumed := end
	if consumed < len(src) {
		consumed++
	}
	return span{lineStart, consumed, line}
}

func caddyAdaptSame(timeout time.Duration, before, after []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	wrapped := false
	beforeJSON, err := caddyAdapt(ctx, before, wrapped)
	if err != nil && strings.Contains(err.Error(), "must appear in a site block") {
		wrapped = true
		beforeJSON, err = caddyAdapt(ctx, before, wrapped)
	}
	if err != nil {
		return fmt.Errorf("adapt original: %w", err)
	}
	afterJSON, err := caddyAdapt(ctx, after, wrapped)
	if err != nil {
		return fmt.Errorf("adapt stripped: %w", err)
	}
	if !bytes.Equal(beforeJSON, afterJSON) {
		return errors.New("stripped Caddyfile adapts to different JSON than the source")
	}
	return nil
}

func caddyAdapt(ctx context.Context, src []byte, wrapped bool) ([]byte, error) {
	dir, err := os.MkdirTemp("", "nocomments-caddy-")
	if err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	stubRoot, err := prepareCaddyWorkDir(dir, src, wrapped)
	if err != nil {
		return nil, err
	}
	return adaptWithStubs(ctx, dir, stubRoot)
}

func adaptWithStubs(ctx context.Context, dir, stubRoot string) ([]byte, error) {
	for range caddyMaxStubs {
		stdout, stderr, err := runCaddyAdapt(ctx, dir)
		if err == nil {
			return stdout, nil
		}
		missing := caddyMissingImport.FindSubmatch(stderr)
		if missing == nil {
			return nil, fmt.Errorf("caddy adapt: %w: %s", err, bytes.TrimSpace(stderr))
		}
		if err := writeCaddyStub(stubRoot, string(missing[1])); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("caddy adapt: more than %d missing imports", caddyMaxStubs)
}

func prepareCaddyWorkDir(dir string, src []byte, wrapped bool) (string, error) {
	config := src
	if wrapped {
		config = []byte("http://localhost {\n" + string(src) + "\n}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "Caddyfile"), config, 0o600); err != nil {
		return "", fmt.Errorf("write Caddyfile: %w", err)
	}
	stubRoot := filepath.Join(dir, "stub")
	if err := os.Mkdir(stubRoot, 0o700); err != nil {
		return "", fmt.Errorf("create stub root: %w", err)
	}
	return stubRoot, nil
}

func writeCaddyStub(stubRoot, importPath string) error {
	if !filepath.IsAbs(importPath) {
		return fmt.Errorf("stub relative import %q: only absolute imports can be stubbed", importPath)
	}
	target := filepath.Join(stubRoot, importPath)
	rel, err := filepath.Rel(stubRoot, target)
	if err != nil {
		return fmt.Errorf("stub path %q: %w", importPath, err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("stub path %q escapes the stub root", importPath)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create stub dir for %q: %w", importPath, err)
	}
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		return fmt.Errorf("write stub %q: %w", importPath, err)
	}
	return nil
}

func runCaddyAdapt(ctx context.Context, dir string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-v", dir+":/w:ro", caddyAdaptImage,
		"sh", "-c", "cp -a /w/stub/. / && caddy adapt --adapter caddyfile --config /w/Caddyfile")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}
