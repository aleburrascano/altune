package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	xhtml "golang.org/x/net/html"
)

func init() {
	register(markdownKind)
	register(htmlKind)
}

var markdownKind = kind{
	name:     "markdown",
	match:    matchMarkdownFile,
	comments: markdownComments,
	same:     markdownSame,
}

var htmlKind = kind{
	name:     "html",
	match:    matchHTMLFile,
	comments: htmlComments,
	same:     htmlSame,
}

var htmlCommentOpen = []byte("<!--")

func matchMarkdownFile(path string, _ []byte) bool {
	return filepath.Ext(path) == ".md"
}

func matchHTMLFile(path string, _ []byte) bool {
	return filepath.Ext(path) == ".html"
}

func htmlComments(src []byte) ([]span, error) {
	return htmlCommentSpans(src, 0, len(src)), nil
}

func htmlSame(before, after []byte) error {
	return sameAfterDroppingComments(before, after)
}

func markdownComments(src []byte) ([]span, error) {
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	var spans []span
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if lo, hi, ok := rawHTMLRange(n); ok {
			spans = append(spans, htmlCommentSpans(src, lo, hi)...)
		}
		return ast.WalkContinue, nil
	})
	return spans, err
}

func rawHTMLRange(n ast.Node) (lo, hi int, ok bool) {
	switch node := n.(type) {
	case *ast.HTMLBlock:
		lines := node.Lines()
		if lines.Len() == 0 {
			return 0, 0, false
		}
		hi = lines.At(lines.Len() - 1).Stop
		if node.HasClosure() {
			hi = node.ClosureLine.Stop
		}
		return lines.At(0).Start, hi, true
	case *ast.RawHTML:
		if node.Segments.Len() == 0 {
			return 0, 0, false
		}
		return node.Segments.At(0).Start, node.Segments.At(node.Segments.Len() - 1).Stop, true
	}
	return 0, 0, false
}

func markdownSame(before, after []byte) error {
	renderedBefore, err := renderMarkdown(before)
	if err != nil {
		return err
	}
	renderedAfter, err := renderMarkdown(after)
	if err != nil {
		return err
	}
	return sameAfterDroppingComments(renderedBefore, renderedAfter)
}

func renderMarkdown(src []byte) ([]byte, error) {
	var out bytes.Buffer
	md := goldmark.New(goldmark.WithRendererOptions(html.WithUnsafe()))
	if err := md.Convert(src, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func sameAfterDroppingComments(before, after []byte) error {
	if collapsedWithoutComments(before) != collapsedWithoutComments(after) {
		return errors.New("stripping the comments changed more than the comments")
	}
	return nil
}

func collapsedWithoutComments(src []byte) string {
	var kept bytes.Buffer
	z := xhtml.NewTokenizer(bytes.NewReader(src))
	for z.Next() != xhtml.ErrorToken {
		raw := z.Raw()
		if z.Token().Type == xhtml.CommentToken && bytes.HasPrefix(raw, htmlCommentOpen) {
			continue
		}
		kept.Write(raw)
	}
	return strings.Join(strings.Fields(kept.String()), " ")
}

func htmlCommentSpans(src []byte, lo, hi int) []span {
	var spans []span
	z := xhtml.NewTokenizer(bytes.NewReader(src[lo:hi]))
	offset := lo
	for z.Next() != xhtml.ErrorToken {
		raw := z.Raw()
		start, end := offset, offset+len(raw)
		offset = end
		if z.Token().Type != xhtml.CommentToken || !bytes.HasPrefix(raw, htmlCommentOpen) {
			continue
		}
		start, end = widenToWholeLines(src, start, end)
		spans = append(spans, span{start, end, 1 + bytes.Count(src[:start], []byte("\n"))})
	}
	return spans
}

func widenToWholeLines(src []byte, start, end int) (int, int) {
	lineStart := bytes.LastIndexByte(src[:start], '\n') + 1
	if len(bytes.TrimLeft(src[lineStart:start], " \t")) != 0 {
		return start, end
	}
	lineEnd := len(src)
	if i := bytes.IndexByte(src[end:], '\n'); i >= 0 {
		lineEnd = end + i
	}
	if len(bytes.TrimRight(src[end:lineEnd], " \t\r")) != 0 {
		return start, end
	}
	if lineEnd < len(src) {
		lineEnd++
	}
	return lineStart, lineEnd
}
