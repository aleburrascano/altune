package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

func init() { register(shellKind) }

var shellKind = kind{
	name:     "shell",
	match:    matchShellFile,
	comments: shellComments,
	same:     shellCommentFreePrintsEqual,
}

var shellShebang = regexp.MustCompile(`^#!.*\b(ba)?sh\b`)

func matchShellFile(path string, head []byte) bool {
	if strings.HasSuffix(path, ".sh") {
		return true
	}
	return shellShebang.Match(firstLine(head))
}

func firstLine(head []byte) []byte {
	if idx := bytes.IndexByte(head, '\n'); idx >= 0 {
		return head[:idx]
	}
	return head
}

func shellComments(src []byte) ([]span, error) {
	file, err := parseShell(src, syntax.KeepComments(true))
	if err != nil {
		return nil, err
	}
	spans := collectCommentSpans(src, file)
	if err := ensureNoResidualComment(src, spans); err != nil {
		return nil, err
	}
	return spans, nil
}

func collectCommentSpans(src []byte, file *syntax.File) []span {
	var spans []span
	syntax.Walk(file, func(n syntax.Node) bool {
		c, ok := n.(*syntax.Comment)
		if !ok {
			return true
		}
		if isShebangComment(c) {
			return true
		}
		spans = append(spans, commentSpan(src, c))
		return true
	})
	return spans
}

func ensureNoResidualComment(src []byte, spans []span) error {
	out := deleteSpans(src, spans)
	if line, found := residualCommentLine(out); found {
		return fmt.Errorf("unrecognized comment marker at line %d survives the comment walk", line)
	}
	return nil
}

type shellScanState struct {
	src       []byte
	i, line   int
	wordStart bool
	pending   []heredocSpec
}

func residualCommentLine(src []byte) (int, bool) {
	st := &shellScanState{src: src, line: 1, wordStart: true}
	skipShebangLine(st)
	for st.i < len(st.src) {
		if line, found, abort := shellScanStep(st); abort {
			return line, found
		}
	}
	return 0, false
}

func skipShebangLine(st *shellScanState) {
	if !bytes.HasPrefix(st.src, []byte("#!")) {
		return
	}
	st.i = lineEndOffset(st.src, 0)
	if st.i < len(st.src) {
		st.i++
	}
	st.line = 2
}

func shellScanStep(st *shellScanState) (line int, found, abort bool) {
	c := st.src[st.i]
	switch {
	case c == '\n':
		return shellScanNewline(st)
	case shellScanQuoteOrEscape(st, c):
	case isBraceExpansionStart(st.src, st.i):
		st.i, st.line = skipBraceExpansion(st.src, st.i+2, st.line)
		st.wordStart = false
	case isHerestringStart(st.src, st.i):
		st.i += 3
		st.wordStart = true
	case isHeredocOpStart(st.src, st.i):
		shellScanHeredocOp(st)
	case c == '#' && st.wordStart:
		return st.line, true, true
	default:
		shellScanAdvancePlain(st, c)
	}
	return 0, false, false
}

func shellScanQuoteOrEscape(st *shellScanState, c byte) bool {
	switch c {
	case '\'':
		if isAnsiCQuoteStart(st.src, st.i) {
			st.i, st.line = skipAnsiCQuote(st.src, st.i+1, st.line)
		} else {
			st.i, st.line = skipSingleQuote(st.src, st.i+1, st.line)
		}
	case '"':
		st.i, st.line = skipDoubleQuote(st.src, st.i+1, st.line)
	case '\\':
		st.i += 2
	default:
		return false
	}
	st.wordStart = false
	return true
}

func shellScanAdvancePlain(st *shellScanState, c byte) {
	st.i++
	st.wordStart = c == ' ' || c == '\t' || isShellWordBoundary(c)
}

func shellScanNewline(st *shellScanState) (line int, found, abort bool) {
	st.i++
	st.line++
	st.wordStart = true
	if len(st.pending) == 0 {
		return 0, false, false
	}
	next, nextLine, ok := consumeHeredocs(st.src, st.i, st.line, st.pending)
	if !ok {
		return 0, false, true
	}
	st.i, st.line = next, nextLine
	st.pending = nil
	return 0, false, false
}

func shellScanHeredocOp(st *shellScanState) {
	spec, next, ok := parseHeredocOp(st.src, st.i)
	if !ok {
		st.i++
		st.wordStart = true
		return
	}
	st.pending = append(st.pending, spec)
	st.i = next
	st.wordStart = false
}

func isBraceExpansionStart(src []byte, i int) bool {
	return src[i] == '$' && i+1 < len(src) && src[i+1] == '{'
}

func isHeredocOpStart(src []byte, i int) bool {
	return src[i] == '<' && i+1 < len(src) && src[i+1] == '<'
}

func isHerestringStart(src []byte, i int) bool {
	return src[i] == '<' && i+2 < len(src) && src[i+1] == '<' && src[i+2] == '<'
}

func isShellWordBoundary(c byte) bool {
	switch c {
	case ';', '|', '&', '(', ')', '{', '}':
		return true
	}
	return false
}

type heredocSpec struct {
	delim     string
	stripTabs bool
}

func parseHeredocOp(src []byte, i int) (heredocSpec, int, bool) {
	n := len(src)
	i += 2
	strip := false
	if i < n && src[i] == '-' {
		strip = true
		i++
	}
	i = skipHorizontalSpace(src, i)
	if i >= n {
		return heredocSpec{}, i, false
	}
	if src[i] == '\'' || src[i] == '"' {
		return parseQuotedHeredocDelim(src, i, strip)
	}
	return parseBareHeredocDelim(src, i, strip)
}

func skipHorizontalSpace(src []byte, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i
}

func parseQuotedHeredocDelim(src []byte, i int, strip bool) (heredocSpec, int, bool) {
	n := len(src)
	quote := src[i]
	i++
	start := i
	for i < n && src[i] != quote {
		i++
	}
	if i >= n {
		return heredocSpec{}, i, false
	}
	return heredocSpec{delim: string(src[start:i]), stripTabs: strip}, i + 1, true
}

func parseBareHeredocDelim(src []byte, i int, strip bool) (heredocSpec, int, bool) {
	n := len(src)
	start := i
	for i < n && !isHeredocDelimBoundary(src[i]) {
		if src[i] == '\\' {
			i += 2
			continue
		}
		i++
	}
	if i == start {
		return heredocSpec{}, i, false
	}
	return heredocSpec{delim: strings.ReplaceAll(string(src[start:i]), `\`, ""), stripTabs: strip}, i, true
}

func isHeredocDelimBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '<', '>', ';', '&', '|':
		return true
	}
	return false
}

func consumeHeredocs(src []byte, i, line int, specs []heredocSpec) (int, int, bool) {
	for _, spec := range specs {
		next, nextLine, ok := consumeOneHeredoc(src, i, line, spec)
		if !ok {
			return 0, 0, false
		}
		i, line = next, nextLine
	}
	return i, line, true
}

func consumeOneHeredoc(src []byte, i, line int, spec heredocSpec) (int, int, bool) {
	n := len(src)
	for i < n {
		body := i
		if spec.stripTabs {
			for body < n && src[body] == '\t' {
				body++
			}
		}
		end := lineEndOffset(src, i)
		candidate := strings.TrimSuffix(string(src[body:end]), "\r")
		i = end
		if i < n {
			i++
		}
		line++
		if candidate == spec.delim {
			return i, line, true
		}
	}
	return 0, 0, false
}

func isAnsiCQuoteStart(src []byte, i int) bool {
	return i > 0 && src[i-1] == '$'
}

func skipAnsiCQuote(src []byte, i, line int) (int, int) {
	n := len(src)
	for i < n && src[i] != '\'' {
		switch src[i] {
		case '\\':
			i++
			if i < n && src[i] == '\n' {
				line++
			}
		case '\n':
			line++
		}
		i++
	}
	if i < n {
		i++
	}
	return i, line
}

func skipSingleQuote(src []byte, i, line int) (int, int) {
	n := len(src)
	for i < n && src[i] != '\'' {
		if src[i] == '\n' {
			line++
		}
		i++
	}
	if i < n {
		i++
	}
	return i, line
}

func skipDoubleQuote(src []byte, i, line int) (int, int) {
	n := len(src)
	for i < n && src[i] != '"' {
		switch src[i] {
		case '\\':
			i++
			if i < n && src[i] == '\n' {
				line++
			}
		case '\n':
			line++
		}
		i++
	}
	if i < n {
		i++
	}
	return i, line
}

func skipBraceExpansion(src []byte, i, line int) (int, int) {
	depth := 1
	for i < len(src) && depth > 0 {
		i, line, depth = braceExpansionStep(src, i, line, depth)
	}
	return i, line
}

func braceExpansionStep(src []byte, i, line, depth int) (int, int, int) {
	switch src[i] {
	case '{':
		return i + 1, line, depth + 1
	case '}':
		return i + 1, line, depth - 1
	case '\'', '"':
		ni, nl := braceExpansionSkipQuote(src, i, line)
		return ni, nl, depth
	case '\\':
		return i + 2, line, depth
	case '\n':
		return i + 1, line + 1, depth
	default:
		return i + 1, line, depth
	}
}

func braceExpansionSkipQuote(src []byte, i, line int) (int, int) {
	if src[i] == '"' {
		return skipDoubleQuote(src, i+1, line)
	}
	if isAnsiCQuoteStart(src, i) {
		return skipAnsiCQuote(src, i+1, line)
	}
	return skipSingleQuote(src, i+1, line)
}

func isShebangComment(c *syntax.Comment) bool {
	return c.Hash.Line() == 1 && c.Hash.Col() == 1 && strings.HasPrefix(c.Text, "!")
}

func commentSpan(src []byte, c *syntax.Comment) span {
	start := int(c.Pos().Offset())
	end := int(c.End().Offset())
	line := int(c.Pos().Line())

	lineStart := lineStartOffset(src, start)
	lineEndOfCommentLine := lineEndOffset(src, start)
	if end > lineEndOfCommentLine {
		end = lineEndOfCommentLine
	}

	if isBlank(src[lineStart:start]) && isBlank(src[end:lineEndOfCommentLine]) {
		consumed := lineEndOfCommentLine
		if consumed < len(src) && !lineIsContinuedFromPrevious(src, lineStart) {
			consumed++
		}
		return span{lineStart, consumed, line}
	}
	return span{trimTrailingSpace(src, lineStart, start), end, line}
}

func lineIsContinuedFromPrevious(src []byte, lineStart int) bool {
	pos := lineStart - 1
	if pos < 0 || src[pos] != '\n' {
		return false
	}
	pos--
	if pos >= 0 && src[pos] == '\r' {
		pos--
	}
	return pos >= 0 && src[pos] == '\\'
}

func lineStartOffset(src []byte, offset int) int {
	if idx := bytes.LastIndexByte(src[:offset], '\n'); idx >= 0 {
		return idx + 1
	}
	return 0
}

func lineEndOffset(src []byte, offset int) int {
	if idx := bytes.IndexByte(src[offset:], '\n'); idx >= 0 {
		return offset + idx
	}
	return len(src)
}

func isBlank(b []byte) bool {
	return len(bytes.TrimSpace(b)) == 0
}

func trimTrailingSpace(src []byte, floor, offset int) int {
	for offset > floor && (src[offset-1] == ' ' || src[offset-1] == '\t') {
		offset--
	}
	return offset
}

func parseShell(src []byte, opts ...syntax.ParserOption) (*syntax.File, error) {
	opts = append(opts, syntax.Variant(syntax.LangBash))
	parser := syntax.NewParser(opts...)
	return parser.Parse(bytes.NewReader(src), "")
}

func shellCommentFreePrintsEqual(before, after []byte) error {
	spans, err := shellComments(before)
	if err != nil {
		return fmt.Errorf("parse original: %w", err)
	}
	beforePrint, err := printShellCommentFree(blankCommentSpans(before, spans))
	if err != nil {
		return fmt.Errorf("parse original: %w", err)
	}
	afterPrint, err := printShellCommentFree(after)
	if err != nil {
		return fmt.Errorf("parse stripped: %w", err)
	}
	if beforePrint != afterPrint {
		return errors.New("stripped output differs from the source beyond its comments")
	}
	return nil
}

func blankCommentSpans(src []byte, spans []span) []byte {
	out := append([]byte(nil), src...)
	for _, s := range spans {
		for i := s.start; i < s.end; i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}
	return out
}

func printShellCommentFree(src []byte) (string, error) {
	file, err := parseShell(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	printer := syntax.NewPrinter(syntax.Minify(true))
	if err := printer.Print(&buf, file); err != nil {
		return "", err
	}
	return buf.String(), nil
}
