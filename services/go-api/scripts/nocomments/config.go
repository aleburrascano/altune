package main

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

func init() {
	register(gitPatternKind)
	register(lineConfigKind)
	register(npmrcKind)
}

var gitPatternKind = kind{
	name:     "git-pattern",
	match:    matchGitPatternFile,
	comments: gitPatternComments,
	same:     gitPatternSame,
}

var lineConfigKind = kind{
	name:     "line-config",
	match:    matchLineConfigFile,
	comments: lineConfigComments,
	same:     lineConfigSame,
}

var npmrcKind = kind{
	name:     "npmrc",
	match:    matchNpmrcFile,
	comments: npmrcComments,
	same:     npmrcSame,
}

var envExampleName = regexp.MustCompile(`^\.env.*\.example$`)

func matchGitPatternFile(path string, _ []byte) bool {
	switch filepath.Base(path) {
	case ".gitignore", ".ignore", ".gitattributes", ".dockerignore":
		return true
	}
	return false
}

func matchLineConfigFile(path string, _ []byte) bool {
	base := filepath.Base(path)
	switch base {
	case "CODEOWNERS", ".gitmessage":
		return true
	}
	if envExampleName.MatchString(base) {
		return true
	}
	return strings.HasSuffix(base, ".tsv")
}

func matchNpmrcFile(path string, _ []byte) bool {
	return filepath.Base(path) == ".npmrc"
}

func gitPatternComments(src []byte) ([]span, error) {
	return lineCommentSpans(src, isGitPatternCommentLine), nil
}

func gitPatternSame(before, after []byte) error {
	return lineConfigSameWith(before, after, isGitPatternCommentLine)
}

func lineConfigComments(src []byte) ([]span, error) {
	return lineCommentSpans(src, isLineConfigCommentLine), nil
}

func lineConfigSame(before, after []byte) error {
	return lineConfigSameWith(before, after, isLineConfigCommentLine)
}

func npmrcComments(src []byte) ([]span, error) {
	return lineCommentSpans(src, isNpmrcCommentLine), nil
}

func npmrcSame(before, after []byte) error {
	return lineConfigSameWith(before, after, isNpmrcCommentLine)
}

func isGitPatternCommentLine(content []byte) bool {
	return len(content) > 0 && content[0] == '#'
}

func isLineConfigCommentLine(content []byte) bool {
	trimmed := bytes.TrimLeft(content, " \t")
	return len(trimmed) > 0 && trimmed[0] == '#'
}

func isNpmrcCommentLine(content []byte) bool {
	trimmed := bytes.TrimLeft(content, " \t")
	return len(trimmed) > 0 && (trimmed[0] == '#' || trimmed[0] == ';')
}

func lineCommentSpans(src []byte, isComment func([]byte) bool) []span {
	var spans []span
	n := len(src)
	i, line := 0, 1
	for i < n {
		lineStart := i
		lineEnd := lineEndOffset(src, i)
		if isComment(src[lineStart:lineEnd]) {
			consumed := lineEnd
			if consumed < n {
				consumed++
			}
			spans = append(spans, span{lineStart, consumed, line})
		}
		i = lineEnd
		if i < n {
			i++
		}
		line++
	}
	return spans
}

func lineConfigSameWith(before, after []byte, isComment func([]byte) bool) error {
	beforeLines := splitKeepingLineEnds(before)
	afterLines := splitKeepingLineEnds(after)
	ai := 0
	for _, bl := range beforeLines {
		if isComment(trimLineEnd(bl)) {
			continue
		}
		if ai >= len(afterLines) {
			return fmt.Errorf("stripped output is missing kept line %q", bl)
		}
		if !bytes.Equal(afterLines[ai], bl) {
			return fmt.Errorf("kept line changed: %q became %q", bl, afterLines[ai])
		}
		ai++
	}
	if ai != len(afterLines) {
		return errors.New("stripped output has extra lines beyond the source's kept lines")
	}
	return nil
}

func trimLineEnd(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r"))
}

func splitKeepingLineEnds(src []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			lines = append(lines, src[start:i+1])
			start = i + 1
		}
	}
	if start < len(src) {
		lines = append(lines, src[start:])
	}
	return lines
}
