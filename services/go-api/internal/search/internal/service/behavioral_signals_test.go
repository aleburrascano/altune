package service_test

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
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

func newRanking(t *testing.T, behavior service.BehavioralScorer) *service.RankingExperiments {
	t.Helper()
	ranking, err := service.NewRankingExperiments(service.RankingConfig{Behavior: behavior}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ranking
}

func scoringFor(t *testing.T, store ports.ListeningSignalStore) service.BehavioralScorer {
	t.Helper()
	scoring, err := service.NewBehavioralScoring(service.NewSatisfactionConsumer(store), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	return scoring
}

func TestRankingExperiments_RefuseAMissingBehavioralScorer(t *testing.T) {
	ranking, err := service.NewRankingExperiments(service.RankingConfig{}, nil)

	if ranking != nil || !errors.Is(err, service.ErrBehaviorRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrBehaviorRequired", ranking, err)
	}
}

func TestNewBehavioralScoring_RefusesAMissingConsumer(t *testing.T) {
	scoring, err := service.NewBehavioralScoring(nil, clock.Now)

	if scoring != nil || !errors.Is(err, service.ErrBehavioralConsumerRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrBehavioralConsumerRequired", scoring, err)
	}
}

func TestBehavioralRankingOff_SnapshotNilAndRefreshIsInert(t *testing.T) {
	ranking := newRanking(t, service.NoBehavioralScoring{})
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

type sinceRecordingStore struct{ since time.Time }

func (s *sinceRecordingStore) SatisfactionSignals(_ context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
	s.since = since
	return nil, nil
}

func TestNewBehavioralScoring_RefusesANilClock(t *testing.T) {
	scoring, err := service.NewBehavioralScoring(service.NewSatisfactionConsumer(&fakeSignalStore{}), nil)

	if scoring != nil || !errors.Is(err, service.ErrBehavioralConsumerRequired) {
		t.Fatalf("got (%v, %v), want a refusal wrapping ErrBehavioralConsumerRequired", scoring, err)
	}
}

func TestBehavioralScoring_ReadsTheLookbackWindowFromTheInjectedClock(t *testing.T) {
	now := time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	store := &sinceRecordingStore{}
	scoring, err := service.NewBehavioralScoring(service.NewSatisfactionConsumer(store), clocktest.NewManual(now).Func())
	if err != nil {
		t.Fatal(err)
	}

	if err := scoring.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if want := now.Add(-30 * 24 * time.Hour); !store.since.Equal(want) {
		t.Errorf("since = %v, want %v, thirty days before the injected clock", store.since, want)
	}
}
