package main

import (
	"bytes"
	"fmt"
)

type tokenClass int

const (
	tokenCode tokenClass = iota
	tokenSpace
	tokenNewline
	tokenComment
	tokenCommentNewline
)

type scanToken struct {
	class tokenClass
	text  []byte
	start int
}

type commentRange struct{ start, end int }

func commentRanges(tokens []scanToken) []commentRange {
	var ranges []commentRange
	for _, tok := range tokens {
		if tok.class == tokenComment || tok.class == tokenCommentNewline {
			ranges = append(ranges, commentRange{tok.start, tok.start + len(tok.text)})
		}
	}
	return ranges
}

func mergeInlineRuns(src []byte, ranges []commentRange) []commentRange {
	var merged []commentRange
	for _, r := range ranges {
		last := len(merged) - 1
		if last >= 0 && isInlineGap(src[merged[last].end:r.start]) {
			merged[last].end = r.end
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func isInlineGap(gap []byte) bool {
	return len(bytes.Trim(gap, " \t")) == 0
}

func blockCommentSpans(src []byte, start, end int) []span {
	line := 1 + bytes.Count(src[:start], []byte{'\n'})
	lineStart := lineStartOffset(src, start)
	lineEnd := lineEndOffset(src, end)
	before, after := isBlank(src[lineStart:start]), isBlank(src[end:lineEnd])
	switch {
	case before && after:
		return []span{{lineStart, nextLineStart(src, lineEnd), line}}
	case before:
		return []span{{start, skipBlanks(src, end, len(src)), line}}
	case after:
		return []span{{trimTrailingSpace(src, lineStart, start), end, line}}
	case start > lineStart && isBlankByte(src[start-1]):
		return []span{{start, skipBlanks(src, end, len(src)), line}}
	default:
		return []span{{start, end, line}}
	}
}

func blockCommentSpansKeepingNewline(src []byte, start, end int) []span {
	spans := blockCommentSpans(src, start, end)
	newline := bytes.IndexByte(src[start:end], '\n')
	lineStart := lineStartOffset(src, start)
	hasCodeAround := !isBlank(src[lineStart:start]) && !isBlank(src[end:lineEndOffset(src, end)])
	if newline < 0 || !hasCodeAround {
		return spans
	}
	keepFrom := start + newline
	if src[keepFrom-1] == '\r' {
		keepFrom--
	}
	line := spans[0].line
	return []span{
		{trimTrailingSpace(src, lineStart, start), keepFrom, line},
		{start + newline + 1, skipBlanks(src, end, len(src)), line},
	}
}

func nextLineStart(src []byte, lineEnd int) int {
	if lineEnd < len(src) {
		return lineEnd + 1
	}
	return lineEnd
}

func significantTokens(tokens []scanToken) []scanToken {
	var out []scanToken
	for _, tok := range tokens {
		switch tok.class {
		case tokenSpace, tokenComment:
			continue
		case tokenNewline, tokenCommentNewline:
			tok = scanToken{class: tokenNewline}
			if len(out) == 0 || out[len(out)-1].class == tokenNewline {
				continue
			}
		}
		out = append(out, tok)
	}
	if len(out) > 0 && out[len(out)-1].class == tokenNewline {
		out = out[:len(out)-1]
	}
	return out
}

func significantTokensEqual(before, after []scanToken) error {
	want, got := significantTokens(before), significantTokens(after)
	for i := 0; i < len(want) && i < len(got); i++ {
		if want[i].class != got[i].class || !bytes.Equal(want[i].text, got[i].text) {
			return fmt.Errorf("token %d changed: %q became %q", i, want[i].text, got[i].text)
		}
	}
	if len(want) != len(got) {
		return fmt.Errorf("token count changed from %d to %d", len(want), len(got))
	}
	return nil
}
