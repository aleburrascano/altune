package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

func init() { register(tomlKind) }

var tomlKind = kind{
	name:     "toml",
	match:    matchTomlFile,
	comments: tomlComments,
	same:     tomlDecodesEqual,
}

func matchTomlFile(path string, _ []byte) bool {
	return strings.HasSuffix(path, ".toml")
}

type tomlScanState struct {
	src       []byte
	i, line   int
	lineStart int
	spans     []span
}

func tomlComments(src []byte) ([]span, error) {
	st := &tomlScanState{src: src, line: 1}
	for st.i < len(st.src) {
		if err := tomlScanStep(st); err != nil {
			return nil, err
		}
	}
	return st.spans, nil
}

func tomlScanStep(st *tomlScanState) error {
	switch c := st.src[st.i]; c {
	case '\n':
		st.i++
		st.line++
		st.lineStart = st.i
	case '"', '\'':
		ni, nl, err := tomlSkipString(st.src, st.i, st.line, c)
		if err != nil {
			return err
		}
		st.i, st.line = ni, nl
	case '#':
		s, next := tomlCommentSpan(st.src, st.lineStart, st.i, st.line)
		st.spans = append(st.spans, s)
		st.i = next
	default:
		st.i++
	}
	return nil
}

func tomlCommentSpan(src []byte, lineStart, hashPos, line int) (span, int) {
	end := lineEndOffset(src, hashPos)
	if isBlank(src[lineStart:hashPos]) {
		consumed := end
		if consumed < len(src) {
			consumed++
		}
		return span{lineStart, consumed, line}, end
	}
	endBeforeCR := end
	if endBeforeCR > hashPos && src[endBeforeCR-1] == '\r' {
		endBeforeCR--
	}
	return span{trimTrailingSpace(src, lineStart, hashPos), endBeforeCR, line}, end
}

func tomlSkipString(src []byte, i, line int, quote byte) (int, int, error) {
	if isTomlTripleQuote(src, i, quote) {
		return tomlSkipMultilineString(src, i, line, quote)
	}
	return tomlSkipSingleLineString(src, i, line, quote)
}

func isTomlTripleQuote(src []byte, i int, quote byte) bool {
	return i+2 < len(src) && src[i+1] == quote && src[i+2] == quote
}

func tomlSkipSingleLineString(src []byte, i, line int, quote byte) (int, int, error) {
	n := len(src)
	startLine := line
	i++
	for i < n {
		switch src[i] {
		case quote:
			return i + 1, line, nil
		case '\\':
			if quote == '"' {
				i += 2
				continue
			}
			i++
		case '\n':
			return 0, 0, fmt.Errorf("unterminated string starting at line %d", startLine)
		default:
			i++
		}
	}
	return 0, 0, fmt.Errorf("unterminated string starting at line %d", startLine)
}

func tomlSkipMultilineString(src []byte, i, line int, quote byte) (int, int, error) {
	n := len(src)
	startLine := line
	i += 3
	for i < n {
		switch src[i] {
		case '\\':
			i, line = tomlSkipMultilineEscape(src, i, line, quote)
		case '\n':
			line++
			i++
		case quote:
			if closeAt, ok := tomlMultilineQuoteRunClose(src, i, quote); ok {
				return closeAt, line, nil
			}
			i = tomlQuoteRunEnd(src, i, quote)
		default:
			i++
		}
	}
	return 0, 0, fmt.Errorf("unterminated multi-line string starting at line %d", startLine)
}

func tomlQuoteRunEnd(src []byte, i int, quote byte) int {
	for i < len(src) && src[i] == quote {
		i++
	}
	return i
}

func tomlMultilineQuoteRunClose(src []byte, i int, quote byte) (int, bool) {
	runEnd := tomlQuoteRunEnd(src, i, quote)
	if runEnd-i >= 3 {
		return runEnd, true
	}
	return 0, false
}

func tomlSkipMultilineEscape(src []byte, i, line int, quote byte) (int, int) {
	if quote != '"' {
		return i + 1, line
	}
	if i+1 < len(src) && src[i+1] == '\n' {
		line++
	}
	return i + 2, line
}

func tomlDecodesEqual(before, after []byte) error {
	var beforeVal, afterVal map[string]any
	if err := toml.Unmarshal(before, &beforeVal); err != nil {
		return fmt.Errorf("decode original: %w", err)
	}
	if err := toml.Unmarshal(after, &afterVal); err != nil {
		return fmt.Errorf("decode stripped: %w", err)
	}
	if !reflect.DeepEqual(beforeVal, afterVal) {
		return errors.New("stripped TOML decodes to a different value tree than the source")
	}
	return nil
}
