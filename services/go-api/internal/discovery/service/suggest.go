package service

import (
	"context"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type SuggestService struct {
	vocab ports.VocabularyStore
}

func NewSuggestService(vocab ports.VocabularyStore) *SuggestService {
	return &SuggestService{vocab: vocab}
}

func (s *SuggestService) Execute(ctx context.Context, partial string, limit int) ([]domain.VocabularyEntry, error) {
	norm := strings.ToLower(strings.TrimSpace(partial))
	if norm == "" {
		return []domain.VocabularyEntry{}, nil
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
	prefix []domain.VocabularyEntry,
) ([]domain.VocabularyEntry, error) {
	remaining := limit - len(prefix)
	fuzzy, err := s.vocab.FindClosest(ctx, norm, remaining+len(prefix))
	if err != nil {
		return prefix, nil
	}
	return deduplicateSuggestions(prefix, fuzzy, limit), nil
}

func deduplicateSuggestions(
	prefix, fuzzy []domain.VocabularyEntry,
	limit int,
) []domain.VocabularyEntry {
	seen := make(map[string]bool, len(prefix))
	for _, e := range prefix {
		seen[e.TermNorm] = true
	}
	combined := append([]domain.VocabularyEntry{}, prefix...)
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
