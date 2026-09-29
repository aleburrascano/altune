package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

func init() { register(sqlKind) }

var sqlKind = kind{
	name:     "sql",
	match:    matchSQLFile,
	comments: sqlComments,
	same:     sqlParsesEqual,
}

var sqlLocationKeys = map[string]bool{"location": true, "stmt_location": true, "stmt_len": true}

func matchSQLFile(path string, _ []byte) bool {
	return strings.HasSuffix(path, ".sql")
}

func sqlComments(src []byte) ([]span, error) {
	scan, err := pgquery.Scan(string(src))
	if err != nil {
		return nil, err
	}
	var spans []span
	for _, tok := range scan.Tokens {
		if tok.Token != pg_query.Token_SQL_COMMENT && tok.Token != pg_query.Token_C_COMMENT {
			continue
		}
		spans = append(spans, sqlCommentSpan(src, int(tok.Start), int(tok.End)))
	}
	return spans, nil
}

func sqlCommentSpan(src []byte, start, end int) span {
	line := bytes.Count(src[:start], []byte{'\n'}) + 1
	lineStart := bytes.LastIndexByte(src[:start], '\n') + 1
	lineEnd := lineEndOffset(src, end)
	if isBlank(src[lineStart:start]) && isBlank(src[end:lineEnd]) {
		consumed := lineEnd
		if consumed < len(src) {
			consumed++
		}
		return span{lineStart, consumed, line}
	}
	return span{trimTrailingSpace(src, lineStart, start), end, line}
}

func sqlParsesEqual(before, after []byte) error {
	beforeTree, err := sqlParseTree(before)
	if err != nil {
		return fmt.Errorf("parse original: %w", err)
	}
	afterTree, err := sqlParseTree(after)
	if err != nil {
		return fmt.Errorf("parse stripped: %w", err)
	}
	if !reflect.DeepEqual(beforeTree, afterTree) {
		return errors.New("stripped SQL parses to a different tree than the source")
	}
	return nil
}

func sqlParseTree(src []byte) (any, error) {
	raw, err := pgquery.ParseToJSON(string(src))
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal([]byte(raw), &tree); err != nil {
		return nil, err
	}
	return sqlDropLocations(tree), nil
}

func sqlDropLocations(v any) any {
	switch n := v.(type) {
	case map[string]any:
		for k, child := range n {
			if sqlLocationKeys[k] {
				delete(n, k)
				continue
			}
			n[k] = sqlDropLocations(child)
		}
	case []any:
		for i, child := range n {
			n[i] = sqlDropLocations(child)
		}
	}
	return v
}
