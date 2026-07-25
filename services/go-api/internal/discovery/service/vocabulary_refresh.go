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

type VocabularyRefreshService struct {
	charts   []ports.ChartProvider
	vocab    ports.VocabularyStore
	interval time.Duration
	limit    int

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
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

func (s *VocabularyRefreshService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
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

func (s *VocabularyRefreshService) Shutdown(ctx context.Context) {
	s.mu.Lock()
	started := s.started
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	if !started {
		return
	}
	select {
	case <-s.done:
	case <-ctx.Done():
		slog.Warn("vocabulary refresh shutdown timed out")
	}
}
