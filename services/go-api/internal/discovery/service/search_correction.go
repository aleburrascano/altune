package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"time"
)

// correctionTimeout is the budget for the vocabulary lookups behind a
// correction, mirroring fillArtwork's explicit budget: a slow or overloaded
// Redis degrades to "no correction" instead of holding the request open.
const correctionTimeout = 1500 * time.Millisecond

func (s *Service) lookupCorrection(ctx context.Context, raw string) *CorrectionResult {
	corrCtx, cancel := context.WithTimeout(ctx, correctionTimeout)
	defer cancel()
	return s.correctionSvc.CorrectAggressive(corrCtx, raw)
}

func (s *Service) tryCorrection(ctx context.Context, query *domain.SearchQuery) (corrected, original string, results []domain.SearchResult, statuses []domain.ProviderSearchResponse) {
	if s.correctionSvc == nil {
		return "", "", nil, nil
	}
	result := s.lookupCorrection(ctx, query.Raw)
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
