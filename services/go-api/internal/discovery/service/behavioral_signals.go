package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/ports"
)

// behavioralLookback bounds how far back the satisfaction signal looks. Recent
// behavior only — older engagement is stale relative to catalog/taste drift.
const behavioralLookback = 30 * 24 * time.Hour

// SatisfactionConsumer is the first EventConsumer: it turns a play (listen
// threshold crossed) or play-to-completion into a positive ranking signal and a
// skip-after-click (short dwell) into a negative, keyed by result_signature.
// New behavioral signals (#3 corpus labels, #5 pogo-sticking) plug in as sibling
// EventConsumers — "add an implementation", never "rewire the pipeline".
type SatisfactionConsumer struct {
	store ports.BehavioralSignalStore
}

func NewSatisfactionConsumer(store ports.BehavioralSignalStore) *SatisfactionConsumer {
	return &SatisfactionConsumer{store: store}
}

func (c *SatisfactionConsumer) Name() string { return "satisfaction" }

func (c *SatisfactionConsumer) Signals(ctx context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
	return c.store.SatisfactionSignals(ctx, since)
}

var _ ports.EventConsumer = (*SatisfactionConsumer)(nil)

// RefreshBehavioralScores recomputes the behavioral score map from the consumer
// and atomically swaps it in. Called off the request path (a background ticker);
// the search path only ever reads the published snapshot. A no-op when behavioral
// ranking is not configured.
func (s *Service) RefreshBehavioralScores(ctx context.Context) error {
	if s.behavioralConsumer == nil {
		return nil
	}
	signals, err := s.behavioralConsumer.Signals(ctx, time.Now().UTC().Add(-behavioralLookback))
	if err != nil {
		return err
	}
	scores := make(map[string]float64, len(signals))
	for _, sig := range signals {
		scores[sig.ResultSignature] = sig.Score
	}
	s.behavioralScores.Store(&scores)
	slog.InfoContext(ctx, "discovery.behavioral_scores_refreshed",
		"consumer", s.behavioralConsumer.Name(), "signatures", len(scores))
	return nil
}

// StartBehavioralRefresh runs RefreshBehavioralScores once immediately, then on
// the given interval until ctx is cancelled. The ticker goroutine is tracked on
// bgWg so graceful shutdown can wait for it. A no-op when behavioral ranking is
// not configured. Best-effort: a refresh error is logged, never fatal — the
// search path keeps serving the last good (or empty) snapshot.
func (s *Service) StartBehavioralRefresh(ctx context.Context, interval time.Duration) {
	if s.behavioralConsumer == nil {
		return
	}
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		if err := s.RefreshBehavioralScores(ctx); err != nil {
			slog.WarnContext(ctx, "discovery.behavioral_refresh_failed", "error", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RefreshBehavioralScores(ctx); err != nil {
					slog.WarnContext(ctx, "discovery.behavioral_refresh_failed", "error", err)
				}
			}
		}
	}()
}

// behavioralScoresSnapshot returns the currently published score map (nil when
// disabled or not yet refreshed). nil-safe: ranking reads it without locking.
func (s *Service) behavioralScoresSnapshot() map[string]float64 {
	if !s.behavioralRanking {
		return nil
	}
	if p := s.behavioralScores.Load(); p != nil {
		return *p
	}
	return nil
}
