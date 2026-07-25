package service

import (
	"context"
	"log/slog"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

func (s *Service) tryCorrection(ctx context.Context, query *domain.SearchQuery) (corrected, original string, results []domain.SearchResult, statuses []domain.ProviderSearchResponse) {
	if s.correctionSvc == nil {
		return "", "", nil, nil
	}
	result := s.correctionSvc.CorrectAggressive(ctx, query.Raw)
	if result == nil {
		return "", "", nil, nil
	}
	corrNorm := textnorm.NormalizeForMatch(result.Corrected)
	if corrNorm == textnorm.NormalizeForMatch(query.Raw) {
		return "", "", nil, nil
	}

	slog.InfoContext(ctx, "search.v2.correcting",
		"original", query.Raw,
		"corrected", result.Corrected,
		"confidence", result.Confidence,
	)

	perProvider, corrStatuses := s.fanOut(ctx, result.Corrected, query.Kinds)
	results = s.mergeRankEnrich(ctx, perProvider, corrNorm)
	if len(results) == 0 {
		return "", "", nil, nil
	}
	return result.Corrected, query.Raw, results, corrStatuses
}
