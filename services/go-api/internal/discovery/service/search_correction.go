package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// tryCorrection runs only when the first search returned nothing: aggressively
// correct the query from the learned vocabulary and re-run the pipeline. The
// trigger is principled (zero results, not a tuned threshold). Returns the
// corrected + original query (for the wire) and the corrected results.
func (s *Service) tryCorrection(ctx context.Context, query *domain.SearchQuery) (corrected, original string, results []domain.SearchResult) {
	if s.correctionSvc == nil {
		return "", "", nil
	}
	result := s.correctionSvc.CorrectAggressive(ctx, query.Raw)
	if result == nil {
		return "", "", nil
	}
	corrNorm := textnorm.NormalizeForMatch(result.Corrected)
	if corrNorm == textnorm.NormalizeForMatch(query.Raw) {
		return "", "", nil
	}

	slog.InfoContext(ctx, "search.v2.correcting",
		"original", query.Raw,
		"corrected", result.Corrected,
		"confidence", result.Confidence,
	)

	perProvider, _ := s.fanOut(ctx, result.Corrected, query.Kinds)
	results = s.mergeRankEnrich(ctx, perProvider, corrNorm)
	if len(results) == 0 {
		return "", "", nil
	}
	return result.Corrected, query.Raw, results
}
