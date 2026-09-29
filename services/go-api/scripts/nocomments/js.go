package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

func init() { register(jsKind) }

var jsKind = kind{
	name:     "js",
	match:    matchJSFile,
	comments: jsComments,
	same:     jsCommentFreeTokensEqual,
}

var jsOwnedByOtherTools = []string{"/apps/mobile/", "/services/overseer/web/", "/node_modules/"}

func matchJSFile(path string, _ []byte) bool {
	switch filepath.Ext(path) {
	case ".js", ".mjs", ".cjs":
	default:
		return false
	}
	slashed := "/" + filepath.ToSlash(path)
	for _, owned := range jsOwnedByOtherTools {
		if strings.Contains(slashed, owned) {
			return false
		}
	}
	return true
}

func jsSourceBuffer(src []byte) []byte {
	buf := make([]byte, len(src), len(src)+1)
	copy(buf, src)
	if bytes.HasPrefix(buf, []byte("#!")) {
		for i := 0; i < len(buf) && buf[i] != '\n' && buf[i] != '\r'; i++ {
			buf[i] = ' '
		}
	}
	return buf
}

type regexCollector struct {
	buf    []byte
	starts map[int]bool
}

func (c *regexCollector) Enter(n js.INode) js.IVisitor {
	lit, ok := n.(*js.LiteralExpr)
	if ok && lit.TokenType == js.RegExpToken && len(lit.Data) > 0 {
		c.starts[offsetOf(c.buf, &lit.Data[0])] = true
	}
	return c
}

func (c *regexCollector) Exit(js.INode) {}

func offsetOf(buf []byte, first *byte) int {
	for i := range buf {
		if &buf[i] == first {
			return i
		}
	}
	return -1
}

func jsRegexStarts(src []byte) (map[int]bool, error) {
	buf := jsSourceBuffer(src)
	tree, err := js.Parse(parse.NewInputBytes(buf), js.Options{})
	if err != nil {
		return nil, err
	}
	collector := &regexCollector{buf: buf, starts: map[int]bool{}}
	js.Walk(collector, tree)
	return collector.starts, nil
}

func jsTokens(src []byte) ([]scanToken, error) {
	regexStarts, err := jsRegexStarts(src)
	if err != nil {
		return nil, err
	}
	input := parse.NewInputBytes(jsSourceBuffer(src))
	lexer := js.NewLexer(input)
	var tokens []scanToken
	for {
		tok, err := nextJSToken(lexer, input, regexStarts)
		if errors.Is(err, io.EOF) {
			return tokens, nil
		}
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
	}
}

func nextJSToken(lexer *js.Lexer, input *parse.Input, regexStarts map[int]bool) (scanToken, error) {
	tokenType, tokenBytes := lexer.Next()
	start := input.Offset() - len(tokenBytes)
	if (tokenType == js.DivToken || tokenType == js.DivEqToken) && regexStarts[start] {
		tokenType, tokenBytes = lexer.RegExp()
	}
	if tokenType == js.ErrorToken {
		return scanToken{}, lexer.Err()
	}
	return scanToken{jsTokenClass(tokenType), tokenBytes, start}, nil
}

func jsTokenClass(tokenType js.TokenType) tokenClass {
	switch tokenType {
	case js.WhitespaceToken:
		return tokenSpace
	case js.LineTerminatorToken:
		return tokenNewline
	case js.CommentToken:
		return tokenComment
	case js.CommentLineTerminatorToken:
		return tokenCommentNewline
	default:
		return tokenCode
	}
}

func jsComments(src []byte) ([]span, error) {
	tokens, err := jsTokens(src)
	if err != nil {
		return nil, err
	}
	var spans []span
	for _, r := range mergeInlineRuns(src, commentRanges(tokens)) {
		spans = append(spans, blockCommentSpansKeepingNewline(src, r.start, r.end)...)
	}
	return spans, nil
}

func jsCommentFreeTokensEqual(before, after []byte) error {
	beforeTokens, err := jsTokens(before)
	if err != nil {
		return fmt.Errorf("lex original: %w", err)
	}
	afterTokens, err := jsTokens(after)
	if err != nil {
		return fmt.Errorf("lex stripped: %w", err)
	}
	return significantTokensEqual(beforeTokens, afterTokens)
}
