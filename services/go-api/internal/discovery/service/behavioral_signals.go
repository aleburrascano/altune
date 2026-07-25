package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/ports"
)

const behavioralLookback = 30 * 24 * time.Hour

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

func (s *Service) BehavioralScoresSnapshot() map[string]float64 {
	if !s.behavioralRanking {
		return nil
	}
	if p := s.behavioralScores.Load(); p != nil {
		return *p
	}
	return nil
}
