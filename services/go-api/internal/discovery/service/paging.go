package service

import "altune/go-api/internal/discovery/domain"

// pageOf slices the ranked list to one page. A non-positive limit means "the
// rest from offset"; an offset past the end yields nil.
func pageOf(ranked []domain.SearchResult, offset, limit int) []domain.SearchResult {
	if offset >= len(ranked) {
		return nil
	}
	end := offset + limit
	if limit <= 0 || end > len(ranked) {
		end = len(ranked)
	}
	return ranked[offset:end]
}
