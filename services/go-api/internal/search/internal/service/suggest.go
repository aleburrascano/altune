package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/music"
	"context"
	"log/slog"
)

type SuggestService struct {
	vocab ports.SpellingHintsReader
}

func NewSuggestService(vocab ports.SpellingHintsReader) *SuggestService {
	return &SuggestService{vocab: vocab}
}

func (s *SuggestService) Execute(ctx context.Context, partial string, limit int) ([]domain.SpellingHintsEntry, error) {
	norm := music.NormalizeForMatch(partial)
	if norm == "" {
		return []domain.SpellingHintsEntry{}, nil
	}

	results, err := s.vocab.SuggestByPrefix(ctx, norm, limit)
	if err != nil {
		return nil, err
	}
	if len(results) >= limit {
		return results, nil
	}

	return s.supplementWithFuzzy(ctx, norm, limit, results)
}

func (s *SuggestService) supplementWithFuzzy(
	ctx context.Context,
	norm string,
	limit int,
	prefix []domain.SpellingHintsEntry,
) ([]domain.SpellingHintsEntry, error) {
	remaining := limit - len(prefix)
	fuzzy, err := s.vocab.FindClosest(ctx, norm, remaining+len(prefix))
	if err != nil {
		slog.DebugContext(ctx, "suggest.fuzzy_supplement_failed", logging.KeyError, err)
		return prefix, nil
	}
	return deduplicateSuggestions(prefix, fuzzy, limit), nil
}

func deduplicateSuggestions(
	prefix, fuzzy []domain.SpellingHintsEntry,
	limit int,
) []domain.SpellingHintsEntry {
	seen := make(map[string]bool, len(prefix))
	for _, e := range prefix {
		seen[e.TermNorm] = true
	}
	combined := append([]domain.SpellingHintsEntry{}, prefix...)
	for _, e := range fuzzy {
		if len(combined) >= limit {
			break
		}
		if seen[e.TermNorm] {
			continue
		}
		seen[e.TermNorm] = true
		combined = append(combined, e)
	}
	return combined
}
