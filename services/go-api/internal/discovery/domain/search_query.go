package domain

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type SearchQuery struct {
	Raw    string
	Kinds  ResultKindSet
	Limit  int
	Offset int
}

const MaxSearchQueryRunes = 200

const MaxSearchQueryTokens = 32

func NewSearchQuery(raw string, kinds ResultKindSet, limit int) (*SearchQuery, error) {
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

func NewPagedSearchQuery(raw string, kinds ResultKindSet, limit, offset int) (*SearchQuery, error) {
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

type ResultKindSet map[ResultKind]bool

func AllKinds() ResultKindSet {
	return ResultKindSet{
		ResultKindTrack:  true,
		ResultKindAlbum:  true,
		ResultKindArtist: true,
	}
}

func (s ResultKindSet) Has(k ResultKind) bool {
	return s[k]
}

func (s ResultKindSet) Key() string {
	ks := make([]string, 0, len(s))
	for k := range s {
		ks = append(ks, k.String())
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}
