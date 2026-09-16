package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
)

// VocabularyRefreshService refreshes the search vocabulary from chart
// providers. It does no scheduling of its own: the app's leader ticker job
// (jobVocabularyRefresh) calls RunOnce and owns the interval, panic recovery
// and shutdown drain.
type VocabularyRefreshService struct {
	charts []ports.ChartProvider
	vocab  ports.VocabularyWriter
	limit  int
}

func NewVocabularyRefreshService(
	charts []ports.ChartProvider,
	vocab ports.VocabularyWriter,
	limit int,
) *VocabularyRefreshService {
	return &VocabularyRefreshService{
		charts: charts,
		vocab:  vocab,
		limit:  limit,
	}
}

const maxVocabEntries = 50000

func (s *VocabularyRefreshService) RunOnce(ctx context.Context) error {
	entries := s.collectEntries(ctx)
	if len(entries) == 0 {
		s.trim(ctx)
		return nil
	}
	if err := s.normalizeAndStore(ctx, entries); err != nil {
		return err
	}
	s.trim(ctx)
	return nil
}

func (s *VocabularyRefreshService) trim(ctx context.Context) {
	if err := s.vocab.Trim(ctx, maxVocabEntries); err != nil {
		slog.Warn("vocabulary trim failed", "error", err)
	}
}

func (s *VocabularyRefreshService) collectEntries(
	ctx context.Context,
) []domain.VocabularyEntry {
	var all []domain.VocabularyEntry
	for _, cp := range s.charts {
		items, err := cp.FetchCharts(ctx, s.limit)
		if err != nil {
			slog.Warn("chart fetch failed", "error", err)
			continue
		}
		all = append(all, items...)
	}
	return all
}

func (s *VocabularyRefreshService) normalizeAndStore(
	ctx context.Context,
	entries []domain.VocabularyEntry,
) error {
	for i := range entries {
		entries[i].TermNorm = textnorm.NormalizeForMatch(entries[i].Term)
	}
	slog.Info("vocabulary refresh", "entries", len(entries))
	return s.vocab.BulkAdd(ctx, entries)
}
