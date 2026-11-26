package rank_test

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/rank"
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
	ranking := newRanking(t, scoringFor(t, store))

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

func newRanking(t *testing.T, behavior rank.BehavioralScorer) *rank.RankingExperiments {
	t.Helper()
	ranking, err := rank.NewRankingExperiments(rank.RankingConfig{Behavior: behavior}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ranking
}

func scoringFor(t *testing.T, store ports.ListeningSignalStore) rank.BehavioralScorer {
	t.Helper()
	scoring, err := rank.NewBehavioralScoring(rank.NewSatisfactionConsumer(store))
	if err != nil {
		t.Fatal(err)
	}
	return scoring
}

func TestRankingExperiments_RefuseAMissingBehavioralScorer(t *testing.T) {
	ranking, err := rank.NewRankingExperiments(rank.RankingConfig{}, nil)

	if ranking != nil || !errors.Is(err, rank.ErrBehaviorRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrBehaviorRequired", ranking, err)
	}
}

func TestNewBehavioralScoring_RefusesAMissingConsumer(t *testing.T) {
	scoring, err := rank.NewBehavioralScoring(nil)

	if scoring != nil || !errors.Is(err, rank.ErrBehavioralConsumerRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrBehavioralConsumerRequired", scoring, err)
	}
}

func TestBehavioralRankingOff_SnapshotNilAndRefreshIsInert(t *testing.T) {
	ranking := newRanking(t, rank.NoBehavioralScoring{})
	if err := ranking.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
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

func TestRefreshBehavioralScores_ErrorKeepsLastSnapshot(t *testing.T) {
	store := &mutableSignalStore{signals: []ports.BehavioralSignal{{ResultSignature: "sig", Score: 2}}}
	ranking := newRanking(t, scoringFor(t, store))

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
