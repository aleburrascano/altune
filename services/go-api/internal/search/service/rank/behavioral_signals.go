package rank

import (
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
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

type BehavioralScorer interface {
	Refresh(ctx context.Context) error
	Snapshot() map[string]float64
}

type NoBehavioralScoring struct{}

func (NoBehavioralScoring) Refresh(context.Context) error { return nil }

func (NoBehavioralScoring) Snapshot() map[string]float64 { return nil }

type listeningScoring struct {
	consumer *ListeningFeedback
	scores   atomic.Pointer[map[string]float64]
}

func NewBehavioralScoring(consumer *ListeningFeedback) (BehavioralScorer, error) {
	if consumer == nil {
		return nil, ErrBehavioralConsumerRequired
	}
	return &listeningScoring{consumer: consumer}, nil
}

var ErrBehavioralConsumerRequired = errors.New("behavioral scoring needs its listening feedback consumer")

func (l *listeningScoring) Refresh(ctx context.Context) error {
	signals, err := l.consumer.Signals(ctx, time.Now().UTC().Add(-behavioralLookback))
	if err != nil {
		return err
	}
	scores := make(map[string]float64, len(signals))
	for _, sig := range signals {
		scores[sig.ResultSignature] = sig.Score
	}
	l.scores.Store(&scores)
	slog.InfoContext(ctx, "discovery.behavioral_scores_refreshed",
		"consumer", l.consumer.Name(), "signatures", len(scores))
	return nil
}

func (l *listeningScoring) Snapshot() map[string]float64 {
	if p := l.scores.Load(); p != nil {
		return *p
	}
	return nil
}

func (r *RankingExperiments) RefreshBehavioralScores(ctx context.Context) error {
	return r.behavior.Refresh(ctx)
}
