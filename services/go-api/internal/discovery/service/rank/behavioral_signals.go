package rank

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

func (r *RankingExperiments) RefreshBehavioralScores(ctx context.Context) error {
	if r.BehavioralConsumer == nil {
		return nil
	}
	signals, err := r.BehavioralConsumer.Signals(ctx, time.Now().UTC().Add(-behavioralLookback))
	if err != nil {
		return err
	}
	scores := make(map[string]float64, len(signals))
	for _, sig := range signals {
		scores[sig.ResultSignature] = sig.Score
	}
	r.behavioralScores.Store(&scores)
	slog.InfoContext(ctx, "discovery.behavioral_scores_refreshed",
		"consumer", r.BehavioralConsumer.Name(), "signatures", len(scores))
	return nil
}

func (r *RankingExperiments) behavioralScoresSnapshot() map[string]float64 {
	if !r.BehavioralRanking {
		return nil
	}
	if p := r.behavioralScores.Load(); p != nil {
		return *p
	}
	return nil
}
