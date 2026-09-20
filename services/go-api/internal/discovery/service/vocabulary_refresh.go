package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
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
	entries, fetchErrs := s.collectEntries(ctx)
	if err := s.storeEntries(ctx, entries); err != nil {
		return err
	}
	s.trim(ctx)
	return chartOutage(len(s.charts), fetchErrs)
}

// chartOutage is the refresh's failure signal. With every provider down the
// stored vocabulary silently goes stale while suggest and correction keep
// serving it, so the job-health record has to see a failed run rather than an
// empty success.
func chartOutage(providerCount int, fetchErrs []error) error {
	everyProviderFailed := providerCount > 0 && len(fetchErrs) == providerCount
	if !everyProviderFailed {
		return nil
	}
	return fmt.Errorf("chart fetch failed for all %d providers: %w",
		providerCount, errors.Join(fetchErrs...))
}

func (s *VocabularyRefreshService) trim(ctx context.Context) {
	if err := s.vocab.Trim(ctx, maxVocabEntries); err != nil {
		slog.Warn("vocabulary trim failed", "error", err)
	}
}

// collectEntries returns what the charts yielded and one error per provider
// that failed. A provider's failure carries its cause as redacted text rather
// than a wrapped error: a chart URL holds the provider's api_key, and this
// error is logged by the job runner.
func (s *VocabularyRefreshService) collectEntries(
	ctx context.Context,
) ([]domain.VocabularyEntry, []error) {
	var all []domain.VocabularyEntry
	var fetchErrs []error
	for _, cp := range s.charts {
		items, err := cp.FetchCharts(ctx, s.limit)
		if err != nil {
			provider := cp.Name().String()
			reason := redact.Secrets(err.Error())
			slog.WarnContext(ctx, "chart fetch failed", "provider", provider, "error", reason)
			fetchErrs = append(fetchErrs, fmt.Errorf("%s: %s", provider, reason))
			continue
		}
		all = append(all, items...)
	}
	return all, fetchErrs
}

func (s *VocabularyRefreshService) storeEntries(
	ctx context.Context,
	entries []domain.VocabularyEntry,
) error {
	if len(entries) == 0 {
		return nil
	}
	return s.normalizeAndStore(ctx, entries)
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
