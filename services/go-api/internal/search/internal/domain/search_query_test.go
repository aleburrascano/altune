package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"strings"
	"testing"
)

func TestNewSearchQuery_Valid(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		kinds map[domain.ResultKind]bool
		limit int
	}{
		{
			name:  "typical query",
			raw:   "radiohead",
			kinds: map[domain.ResultKind]bool{domain.ResultKindTrack: true},
			limit: 25,
		},
		{
			name:  "limit lower bound",
			raw:   "query",
			kinds: map[domain.ResultKind]bool{domain.ResultKindArtist: true},
			limit: 1,
		},
		{
			name:  "limit upper bound",
			raw:   "query",
			kinds: map[domain.ResultKind]bool{domain.ResultKindAlbum: true},
			limit: 50,
		},
		{
			name: "multiple kinds",
			raw:  "beatles",
			kinds: map[domain.ResultKind]bool{
				domain.ResultKindTrack:  true,
				domain.ResultKindAlbum:  true,
				domain.ResultKindArtist: true,
			},
			limit: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := domain.NewSearchQuery(tt.raw, tt.kinds, tt.limit)
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
	validKinds := map[domain.ResultKind]bool{domain.ResultKindTrack: true}

	tests := []struct {
		name    string
		raw     string
		kinds   map[domain.ResultKind]bool
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
			kinds:   map[domain.ResultKind]bool{},
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
			raw:     strings.Repeat("a", domain.MaxSearchQueryRunes+1),
			kinds:   validKinds,
			limit:   10,
			wantMsg: "raw query must be at most 200 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q, err := domain.NewSearchQuery(tt.raw, tt.kinds, tt.limit)
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
	kinds := map[domain.ResultKind]bool{domain.ResultKindTrack: true}
	words := func(n int) string { return strings.TrimSpace(strings.Repeat("a ", n)) }

	if _, err := domain.NewSearchQuery(words(domain.MaxSearchQueryTokens), kinds, 10); err != nil {
		t.Fatalf("query at the token cap rejected: %v", err)
	}
	if _, err := domain.NewSearchQuery("Symphony No. 9 in D minor, Op. 125 Choral: IV. Presto", kinds, 10); err != nil {
		t.Fatalf("long real-world title rejected: %v", err)
	}
	over := words(domain.MaxSearchQueryTokens + 1)
	_, err := domain.NewPagedSearchQuery(over, kinds, 10, 0)
	if err == nil || err.Error() != "raw query must be at most 32 words" {
		t.Fatalf("NewPagedSearchQuery(%d words) err = %v, want word-cap error", domain.MaxSearchQueryTokens+1, err)
	}
}

func TestSearchQueryKinds_Key(t *testing.T) {
	tests := []struct {
		name  string
		kinds map[domain.ResultKind]bool
		want  string
	}{
		{"single track", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, "track"},
		{"sorted regardless of insertion", map[domain.ResultKind]bool{domain.ResultKindTrack: true, domain.ResultKindArtist: true, domain.ResultKindAlbum: true}, "album,artist,track"},
		{"false-valued key still present", map[domain.ResultKind]bool{domain.ResultKindTrack: true, domain.ResultKindAlbum: false}, "album,track"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := domain.NewSearchQuery("radiohead", tt.kinds, 10)
			if err != nil {
				t.Fatal(err)
			}
			if got := q.Kinds.Key(); got != tt.want {
				t.Errorf("Key() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAllKinds_HasEveryKind(t *testing.T) {
	kinds := domain.AllKinds()
	for _, k := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindAlbum, domain.ResultKindArtist} {
		if !kinds.Has(k) {
			t.Errorf("AllKinds() missing %v", k)
		}
	}
	if got := kinds.Key(); got != "album,artist,track" {
		t.Errorf("Key() = %q", got)
	}
}

func TestNewPagedSearchQueryRejectsOutOfRangeOffset(t *testing.T) {
	kinds := map[domain.ResultKind]bool{domain.ResultKindTrack: true}

	if _, err := domain.NewPagedSearchQuery("hello", kinds, 20, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := domain.NewPagedSearchQuery("hello", kinds, 20, domain.MaxSearchOffset+1); err == nil {
		t.Fatal("offset past the cap accepted")
	}
	q, err := domain.NewPagedSearchQuery("hello", kinds, 20, domain.MaxSearchOffset)
	if err != nil {
		t.Fatalf("offset at the cap rejected: %v", err)
	}
	if q.Offset != domain.MaxSearchOffset {
		t.Fatalf("offset = %d, want %d", q.Offset, domain.MaxSearchOffset)
	}
}
