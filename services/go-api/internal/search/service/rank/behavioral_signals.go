package rank

import (
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"log/slog"
	"time"
)

const behavioralLookback = 30 * 24 * time.Hour

type ListeningFeedback struct {
	store ports.ListeningSignalStore
}

func NewSatisfactionConsumer(store ports.ListeningSignalStore) *ListeningFeedback {
	return &ListeningFeedback{store: store}
}

func (c *ListeningFeedback) Name() string { return "satisfaction" }

func (c *ListeningFeedback) Signals(ctx context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
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
