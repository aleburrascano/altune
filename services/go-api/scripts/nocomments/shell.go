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
	return spans, nil
}

func isShebangComment(c *syntax.Comment) bool {
	return c.Hash.Line() == 1 && c.Hash.Col() == 1
}

func commentSpan(src []byte, c *syntax.Comment) span {
	start := int(c.Pos().Offset())
	end := int(c.End().Offset())
	line := int(c.Pos().Line())

	lineStart := lineStartOffset(src, start)
	lineEnd := lineEndOffset(src, end)

	if isBlank(src[lineStart:start]) && isBlank(src[end:lineEnd]) {
		return span{lineStart, lineEnd, line}
	}
	return span{trimTrailingSpace(src, lineStart, start), end, line}
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
	beforePrint, err := printShellCommentFree(before)
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

func printShellCommentFree(src []byte) (string, error) {
	file, err := parseShell(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	printer := syntax.NewPrinter()
	if err := printer.Print(&buf, file); err != nil {
		return "", err
	}
	return buf.String(), nil
}
