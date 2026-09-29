package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func init() { register(dockerfileKind) }

var dockerfileKind = kind{
	name:     "dockerfile",
	match:    matchDockerfile,
	comments: dockerfileComments,
	same:     dockerfileSame,
}

func matchDockerfile(path string, _ []byte) bool {
	base := filepath.Base(path)
	return base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || filepath.Ext(base) == ".dockerfile"
}

func dockerfileComments(src []byte) ([]span, error) {
	res, err := parser.Parse(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	lines := bytes.SplitAfter(src, []byte("\n"))
	directiveLines, err := leadingDirectiveLines(lines)
	if err != nil {
		return nil, err
	}
	return commentLineSpans(lines, directiveLines, heredocBodyLines(res.AST)), nil
}

func commentLineSpans(lines [][]byte, directiveLines int, heredocLines map[int]bool) []span {
	var spans []span
	offset := 0
	for i, raw := range lines {
		start := offset
		offset += len(raw)
		lineNo := i + 1
		if lineNo <= directiveLines || heredocLines[lineNo] {
			continue
		}
		if bytes.HasPrefix(trimLine(raw), []byte("#")) {
			spans = append(spans, span{start, offset, lineNo})
		}
	}
	return spans
}

func trimLine(raw []byte) []byte {
	return bytes.TrimLeft(bytes.TrimRight(raw, "\r\n"), " \t")
}

func leadingDirectiveLines(lines [][]byte) (int, error) {
	var directives parser.DirectiveParser
	for i, raw := range lines {
		d, err := directives.ParseLine(trimLine(raw))
		if err != nil {
			return 0, err
		}
		if d == nil {
			return i, nil
		}
	}
	return len(lines), nil
}

func heredocBodyLines(ast *parser.Node) map[int]bool {
	lines := map[int]bool{}
	for _, n := range ast.Children {
		count := 0
		for _, h := range n.Heredocs {
			count += strings.Count(h.Content, "\n") + 1
		}
		for l := n.EndLine - count + 1; l <= n.EndLine; l++ {
			lines[l] = true
		}
	}
	return lines
}

func dockerfileSame(before, after []byte) error {
	b, err := parser.Parse(bytes.NewReader(before))
	if err != nil {
		return fmt.Errorf("parse original: %w", err)
	}
	a, err := parser.Parse(bytes.NewReader(after))
	if err != nil {
		return fmt.Errorf("parse stripped: %w", err)
	}
	if b.AST.Dump() != a.AST.Dump() {
		return errors.New("stripped output changes the instructions")
	}
	if b.EscapeToken != a.EscapeToken {
		return errors.New("stripped output changes the escape token")
	}
	return sameHeredocs(b.AST.Children, a.AST.Children)
}

func sameHeredocs(before, after []*parser.Node) error {
	if len(before) != len(after) {
		return errors.New("stripped output changes the instruction count")
	}
	for i, n := range before {
		if !reflect.DeepEqual(n.Heredocs, after[i].Heredocs) {
			return fmt.Errorf("stripped output changes the heredocs of instruction %d", i+1)
		}
	}
	return nil
}
