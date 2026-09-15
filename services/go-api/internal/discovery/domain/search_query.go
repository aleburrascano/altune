package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type SearchQuery struct {
	Raw    string
	Kinds  map[ResultKind]bool
	Limit  int
	Offset int
}

// MaxSearchQueryRunes caps a raw search query. No real title or artist search
// approaches it, and it bounds the per-query work downstream (fuzzy correction,
// provider fan-out) an oversized query could otherwise trigger.
const MaxSearchQueryRunes = 200

// MaxSearchQueryTokens caps the whitespace-separated words in a raw search
// query. The rune cap alone still admits ~100 one-letter words, all of which
// fan out to every provider and flow into ranking and vocabulary ingestion;
// real title/artist searches stay far below it.
const MaxSearchQueryTokens = 32

func NewSearchQuery(raw string, kinds map[ResultKind]bool, limit int) (*SearchQuery, error) {
	if raw == "" {
		return nil, fmt.Errorf("raw query cannot be empty")
	}
	if utf8.RuneCountInString(raw) > MaxSearchQueryRunes {
		return nil, fmt.Errorf("raw query must be at most %d characters", MaxSearchQueryRunes)
	}
	if len(strings.Fields(raw)) > MaxSearchQueryTokens {
		return nil, fmt.Errorf("raw query must be at most %d words", MaxSearchQueryTokens)
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("kinds cannot be empty")
	}
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("limit must be between 1 and 50")
	}
	return &SearchQuery{
		Raw:   raw,
		Kinds: kinds,
		Limit: limit,
	}, nil
}

const MaxSearchOffset = 200

func NewPagedSearchQuery(raw string, kinds map[ResultKind]bool, limit, offset int) (*SearchQuery, error) {
	q, err := NewSearchQuery(raw, kinds, limit)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset > MaxSearchOffset {
		return nil, fmt.Errorf("offset must be between 0 and %d", MaxSearchOffset)
	}
	q.Offset = offset
	return q, nil
}
