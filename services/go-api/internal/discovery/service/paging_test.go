package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func slate(n int) []domain.SearchResult {
	out := make([]domain.SearchResult, n)
	for i := range out {
		out[i] = domain.SearchResult{Title: string(rune('a' + i))}
	}
	return out
}

func TestPageOf(t *testing.T) {
	tests := []struct {
		name          string
		total         int
		offset, limit int
		wantLen       int
		wantFirst     string
	}{
		{name: "first page", total: 10, offset: 0, limit: 4, wantLen: 4, wantFirst: "a"},
		{name: "second page continues where the first ended", total: 10, offset: 4, limit: 4, wantLen: 4, wantFirst: "e"},
		{name: "last page is short, not padded", total: 10, offset: 8, limit: 4, wantLen: 2, wantFirst: "i"},
		{name: "offset past the end yields nothing", total: 3, offset: 20, limit: 4, wantLen: 0},
		{name: "offset exactly at the end yields nothing", total: 4, offset: 4, limit: 4, wantLen: 0},
		{name: "zero limit means the rest of the slate", total: 5, offset: 1, limit: 0, wantLen: 4, wantFirst: "b"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pageOf(slate(tc.total), tc.offset, tc.limit)
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantLen > 0 && got[0].Title != tc.wantFirst {
				t.Fatalf("first = %q, want %q", got[0].Title, tc.wantFirst)
			}
		})
	}
}

func TestPageOfTilesTheSlateWithoutGapsOrRepeats(t *testing.T) {
	const total, limit = 23, 5
	full := slate(total)

	seen := make([]string, 0, total)
	for offset := 0; offset < total; offset += limit {
		for _, r := range pageOf(full, offset, limit) {
			seen = append(seen, r.Title)
		}
	}

	if len(seen) != total {
		t.Fatalf("paged through %d results, want %d", len(seen), total)
	}
	for i, r := range full {
		if seen[i] != r.Title {
			t.Fatalf("position %d = %q, want %q", i, seen[i], r.Title)
		}
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
