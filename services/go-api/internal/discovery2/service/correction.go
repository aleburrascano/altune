package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// tryCorrection runs only when the first search returned nothing: aggressively
// correct the query from the learned vocabulary and re-run the pipeline. Returns
// the corrected + original query (for the wire) and the corrected results.
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

	legacyIntent, intent := s.detectIntent(ctx, corrNorm)
	perProvider, _ := s.fanOut(ctx, result.Corrected, query.Kinds, legacyIntent)
	results = s.mergeRankEnrich(ctx, perProvider, corrNorm, intent)
	if len(results) == 0 {
		return "", "", nil
	}
	return result.Corrected, query.Raw, results
}

// suggestIfWeak offers a non-forced "did you mean…?" when the top result is only
// a weak match — i.e. it lands below tier T2. Categorical (tier-based), no tuned
// relevance threshold.
func (s *Service) suggestIfWeak(ctx context.Context, results []domain.SearchResult, rawQuery, queryNorm string, intent Intent) string {
	if s.correctionSvc == nil || len(results) == 0 {
		return ""
	}
	target := intent.Title
	if target == "" {
		target = textnorm.NormalizeForMatch(queryNorm)
	}
	if tierOf(results[0], intent, target) >= tierTitleOtherKind {
		return "" // top result is a strong (T1/T2) match — no suggestion needed
	}

	result := s.correctionSvc.Correct(ctx, rawQuery)
	if result == nil {
		return ""
	}
	if textnorm.NormalizeForMatch(result.Corrected) == textnorm.NormalizeForMatch(queryNorm) {
		return ""
	}
	slog.InfoContext(ctx, "search.v2.suggestion",
		"original", rawQuery,
		"suggested", result.Corrected,
		"confidence", result.Confidence,
	)
	return result.Corrected
}
