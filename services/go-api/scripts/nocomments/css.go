package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

func init() { register(cssKind) }

var cssKind = kind{
	name:     "css",
	match:    matchCSSFile,
	comments: cssComments,
	same:     cssCommentFreeTokensEqual,
}

func matchCSSFile(path string, _ []byte) bool {
	return strings.HasSuffix(path, ".css")
}

func cssTokens(src []byte) ([]scanToken, error) {
	input := parse.NewInputBytes(append([]byte(nil), src...))
	lexer := css.NewLexer(input)
	var tokens []scanToken
	for {
		tokenType, tokenBytes := lexer.Next()
		if tokenType == css.ErrorToken {
			if errors.Is(lexer.Err(), io.EOF) {
				return tokens, nil
			}
			return nil, lexer.Err()
		}
		tokens = append(tokens, scanToken{cssTokenClass(tokenType), tokenBytes, input.Offset() - len(tokenBytes)})
	}
}

func cssTokenClass(tokenType css.TokenType) tokenClass {
	switch tokenType {
	case css.WhitespaceToken:
		return tokenSpace
	case css.CommentToken:
		return tokenComment
	default:
		return tokenCode
	}
}

func cssComments(src []byte) ([]span, error) {
	tokens, err := cssTokens(src)
	if err != nil {
		return nil, err
	}
	var spans []span
	for _, r := range mergeInlineRuns(src, commentRanges(tokens)) {
		spans = append(spans, blockCommentSpans(src, r.start, r.end)...)
	}
	return spans, nil
}

func cssCommentFreeTokensEqual(before, after []byte) error {
	beforeTokens, err := cssTokens(before)
	if err != nil {
		return fmt.Errorf("lex original: %w", err)
	}
	afterTokens, err := cssTokens(after)
	if err != nil {
		return fmt.Errorf("lex stripped: %w", err)
	}
	return significantTokensEqual(beforeTokens, afterTokens)
}
