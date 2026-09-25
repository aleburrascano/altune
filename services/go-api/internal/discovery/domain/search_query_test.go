package domain

import (
	"strings"
	"testing"
)

func TestNewSearchQuery_Valid(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		kinds map[ResultKind]bool
		limit int
	}{
		{
			name:  "typical query",
			raw:   "radiohead",
			kinds: map[ResultKind]bool{ResultKindTrack: true},
			limit: 25,
		},
		{
			name:  "limit lower bound",
			raw:   "query",
			kinds: map[ResultKind]bool{ResultKindArtist: true},
			limit: 1,
		},
		{
			name:  "limit upper bound",
			raw:   "query",
			kinds: map[ResultKind]bool{ResultKindAlbum: true},
			limit: 50,
		},
		{
			name: "multiple kinds",
			raw:  "beatles",
			kinds: map[ResultKind]bool{
				ResultKindTrack:  true,
				ResultKindAlbum:  true,
				ResultKindArtist: true,
			},
			limit: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := NewSearchQuery(tt.raw, tt.kinds, tt.limit)
			if err != nil {
				t.Fatalf("NewSearchQuery() unexpected error: %v", err)
			}
			if q.Raw != tt.raw {
				t.Errorf("Raw = %q, want %q", q.Raw, tt.raw)
			}
			if q.Limit != tt.limit {
				t.Errorf("Limit = %d, want %d", q.Limit, tt.limit)
			}
			if len(q.Kinds) != len(tt.kinds) {
				t.Errorf("Kinds length = %d, want %d", len(q.Kinds), len(tt.kinds))
			}
		})
	}
}

func TestNewSearchQuery_Errors(t *testing.T) {
	validKinds := map[ResultKind]bool{ResultKindTrack: true}

	tests := []struct {
		name    string
		raw     string
		kinds   map[ResultKind]bool
		limit   int
		wantMsg string
	}{
		{
			name:    "empty raw",
			raw:     "",
			kinds:   validKinds,
			limit:   10,
			wantMsg: "raw query cannot be empty",
		},
		{
			name:    "empty kinds",
			raw:     "query",
			kinds:   map[ResultKind]bool{},
			limit:   10,
			wantMsg: "kinds cannot be empty",
		},
		{
			name:    "nil kinds",
			raw:     "query",
			kinds:   nil,
			limit:   10,
			wantMsg: "kinds cannot be empty",
		},
		{
			name:    "limit too low",
			raw:     "query",
			kinds:   validKinds,
			limit:   0,
			wantMsg: "limit must be between 1 and 50",
		},
		{
			name:    "limit negative",
			raw:     "query",
			kinds:   validKinds,
			limit:   -1,
			wantMsg: "limit must be between 1 and 50",
		},
		{
			name:    "limit too high",
			raw:     "query",
			kinds:   validKinds,
			limit:   51,
			wantMsg: "limit must be between 1 and 50",
		},
		{
			name:    "raw longer than max length",
			raw:     strings.Repeat("a", MaxSearchQueryRunes+1),
			kinds:   validKinds,
			limit:   10,
			wantMsg: "raw query must be at most 200 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := NewSearchQuery(tt.raw, tt.kinds, tt.limit)
			if err == nil {
				t.Fatalf("NewSearchQuery() expected error containing %q, got nil (result: %+v)", tt.wantMsg, q)
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("error = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestNewSearchQuery_TokenCap(t *testing.T) {
	kinds := map[ResultKind]bool{ResultKindTrack: true}
	words := func(n int) string { return strings.TrimSpace(strings.Repeat("a ", n)) }

	if _, err := NewSearchQuery(words(MaxSearchQueryTokens), kinds, 10); err != nil {
		t.Fatalf("query at the token cap rejected: %v", err)
	}
	if _, err := NewSearchQuery("Symphony No. 9 in D minor, Op. 125 Choral: IV. Presto", kinds, 10); err != nil {
		t.Fatalf("long real-world title rejected: %v", err)
	}
	over := words(MaxSearchQueryTokens + 1)
	_, err := NewPagedSearchQuery(over, kinds, 10, 0)
	if err == nil || err.Error() != "raw query must be at most 32 words" {
		t.Fatalf("NewPagedSearchQuery(%d words) err = %v, want word-cap error", MaxSearchQueryTokens+1, err)
	}
}
