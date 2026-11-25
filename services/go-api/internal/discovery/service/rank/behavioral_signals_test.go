package rank_test

import (
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/rank"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSignalStore struct {
	signals []ports.BehavioralSignal
	err     error
}

func (f *fakeSignalStore) SatisfactionSignals(context.Context, time.Time) ([]ports.BehavioralSignal, error) {
	return f.signals, f.err
}

func TestSatisfactionConsumer_RefreshPublishesScores(t *testing.T) {
	store := &fakeSignalStore{signals: []ports.BehavioralSignal{
		{ResultSignature: "track|hello|adele", Score: 3},
		{ResultSignature: "track|noise|ugc", Score: -2},
	}}
	ranking := rank.NewRankingExperiments(rank.RankingConfig{BehavioralRanking: true, BehavioralConsumer: rank.NewSatisfactionConsumer(store)}, nil)

	if got := ranking.RankOptions().Behavioral; got != nil {
		t.Fatalf("snapshot should be nil before first refresh, got %v", got)
	}
	if err := ranking.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	scores := ranking.RankOptions().Behavioral
	if scores["track|hello|adele"] != 3 || scores["track|noise|ugc"] != -2 {
		t.Errorf("published scores = %v, want hello=3 noise=-2", scores)
	}
}

func TestBehavioralRankingDisabled_SnapshotNil(t *testing.T) {
	store := &fakeSignalStore{signals: []ports.BehavioralSignal{{ResultSignature: "x", Score: 9}}}
	ranking := rank.NewRankingExperiments(rank.RankingConfig{}, nil)
	ranking.BehavioralConsumer = rank.NewSatisfactionConsumer(store)
	if got := ranking.RankOptions().Behavioral; got != nil {
		t.Errorf("snapshot must be nil when behavioral ranking is off, got %v", got)
	}
}

type mutableSignalStore struct {
	mu      sync.Mutex
	signals []ports.BehavioralSignal
	err     error
}

func (f *mutableSignalStore) SatisfactionSignals(context.Context, time.Time) ([]ports.BehavioralSignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signals, f.err
}

func (f *mutableSignalStore) set(signals []ports.BehavioralSignal, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals, f.err = signals, err
}

func TestRefreshBehavioralScores_NoConsumerIsNoop(t *testing.T) {
	ranking := rank.NewRankingExperiments(rank.RankingConfig{}, nil)
	if err := ranking.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatalf("no-consumer refresh must be a nil no-op, got %v", err)
	}
}

func TestRefreshBehavioralScores_ErrorKeepsLastSnapshot(t *testing.T) {
	store := &mutableSignalStore{signals: []ports.BehavioralSignal{{ResultSignature: "sig", Score: 2}}}
	ranking := rank.NewRankingExperiments(rank.RankingConfig{BehavioralRanking: true, BehavioralConsumer: rank.NewSatisfactionConsumer(store)}, nil)

	if err := ranking.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.set(nil, errors.New("db down"))
	if err := ranking.RefreshBehavioralScores(context.Background()); err == nil {
		t.Fatal("refresh must surface the consumer error to its caller")
	}
	if got := ranking.RankOptions().Behavioral; got["sig"] != 2 {
		t.Errorf("snapshot after failed refresh = %v, want the last good map kept", got)
	}
}
