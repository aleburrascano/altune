package service

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
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

// maxVocabEntries caps the learned vocabulary so the Redis index does not grow
// unbounded as new query/result terms are ingested from traffic. Generous — the
// household + chart vocabulary is well under this — and enforced on the periodic
// refresh, the natural home for retention.
const maxVocabEntries = 50000

// RunOnce fetches charts from all providers, ingests them, then trims the
// vocabulary back to its retention bound.
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

// trim invokes the store's owned retention when it supports it. Kept off the
// shared VocabularyStore port (ISP): only this maintenance path needs it, so the
// capability is discovered structurally rather than widening the read/write seam.
func (s *VocabularyRefreshService) trim(ctx context.Context) {
	t, ok := s.vocab.(interface {
		Trim(ctx context.Context, maxEntries int) error
	})
	if !ok {
		return
	}
	if err := t.Trim(ctx, maxVocabEntries); err != nil {
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

// Start launches the background refresh loop.
func (s *VocabularyRefreshService) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.loop(ctx)
}

func (s *VocabularyRefreshService) loop(ctx context.Context) {
	defer close(s.done)
	s.runWithRecover(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runWithRecover(ctx)
		}
	}
}

func (s *VocabularyRefreshService) runWithRecover(ctx context.Context) {
	defer s.recoverPanic()
	s.runSafe(ctx)
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
