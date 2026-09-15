package service

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"log/slog"
	"time"
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

// RefreshBehavioralScores, StartBehavioralRefresh and BehavioralScoresSnapshot
// are the composition root's entry points into the ranking collaborator; they
// stay on Service to preserve the public API and delegate to RankingExperiments.
func (s *Service) RefreshBehavioralScores(ctx context.Context) error {
	return s.ranking.refreshBehavioralScores(ctx)
}

func (s *Service) StartBehavioralRefresh(ctx context.Context, interval time.Duration) {
	s.ranking.startBehavioralRefresh(ctx, interval)
}

func (s *Service) BehavioralScoresSnapshot() map[string]float64 {
	return s.ranking.behavioralScoresSnapshot()
}

func (r *RankingExperiments) refreshBehavioralScores(ctx context.Context) error {
	if r.behavioralConsumer == nil {
		return nil
	}
	signals, err := r.behavioralConsumer.Signals(ctx, time.Now().UTC().Add(-behavioralLookback))
	if err != nil {
		return err
	}
	scores := make(map[string]float64, len(signals))
	for _, sig := range signals {
		scores[sig.ResultSignature] = sig.Score
	}
	r.behavioralScores.Store(&scores)
	slog.InfoContext(ctx, "discovery.behavioral_scores_refreshed",
		"consumer", r.behavioralConsumer.Name(), "signatures", len(scores))
	return nil
}

func (r *RankingExperiments) startBehavioralRefresh(ctx context.Context, interval time.Duration) {
	if r.behavioralConsumer == nil {
		return
	}
	r.bg.track(func() {
		if err := r.refreshBehavioralScores(ctx); err != nil {
			slog.WarnContext(ctx, "discovery.behavioral_refresh_failed", "error", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.refreshBehavioralScores(ctx); err != nil {
					slog.WarnContext(ctx, "discovery.behavioral_refresh_failed", "error", err)
				}
			}
		}
	})
}

func (r *RankingExperiments) behavioralScoresSnapshot() map[string]float64 {
	if !r.behavioralRanking {
		return nil
	}
	if p := r.behavioralScores.Load(); p != nil {
		return *p
	}
	return nil
}
