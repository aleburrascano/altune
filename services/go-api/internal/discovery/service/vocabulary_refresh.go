package service

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// VocabularyRefreshService periodically fetches chart data from
// providers and populates the vocabulary store for autocomplete.
type VocabularyRefreshService struct {
	charts   []ports.ChartProvider
	vocab    ports.VocabularyStore
	interval time.Duration
	limit    int
	cancel   context.CancelFunc
	done     chan struct{}
	once     sync.Once
}

func NewVocabularyRefreshService(
	charts []ports.ChartProvider,
	vocab ports.VocabularyStore,
	interval time.Duration,
	limit int,
) *VocabularyRefreshService {
	return &VocabularyRefreshService{
		charts:   charts,
		vocab:    vocab,
		interval: interval,
		limit:    limit,
		done:     make(chan struct{}),
	}
}

// RunOnce fetches charts from all providers and ingests into vocab.
func (s *VocabularyRefreshService) RunOnce(ctx context.Context) error {
	entries := s.collectEntries(ctx)
	if len(entries) == 0 {
		return nil
	}
	return s.normalizeAndStore(ctx, entries)
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
		entries[i].TermNorm = NormalizeForMatch(entries[i].Term)
	}
	slog.Info("vocabulary refresh", "entries", len(entries))
	return s.vocab.BulkAdd(ctx, entries)
}

// Start launches the background refresh loop.
func (s *VocabularyRefreshService) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.loop(ctx)
}

func (s *VocabularyRefreshService) loop(ctx context.Context) {
	defer close(s.done)
	defer s.recoverPanic()

	s.runSafe(ctx)
	s.tick(ctx)
}

func (s *VocabularyRefreshService) tick(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runSafe(ctx)
		}
	}
}

func (s *VocabularyRefreshService) runSafe(ctx context.Context) {
	if err := s.RunOnce(ctx); err != nil {
		slog.Error("vocabulary refresh failed", "error", err)
	}
}

func (s *VocabularyRefreshService) recoverPanic() {
	if r := recover(); r != nil {
		slog.Error("vocabulary refresh panic",
			"panic", r,
			"stack", string(debug.Stack()),
		)
	}
}

// Shutdown cancels the background loop and waits for it to finish.
func (s *VocabularyRefreshService) Shutdown(ctx context.Context) {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})
	select {
	case <-s.done:
	case <-ctx.Done():
		slog.Warn("vocabulary refresh shutdown timed out")
	}
}
