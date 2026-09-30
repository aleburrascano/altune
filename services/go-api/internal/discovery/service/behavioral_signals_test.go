package service

import (
	"altune/go-api/internal/discovery/ports"
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
	svc := NewService(nil, NewCircuitBreaker(), WithBehavioralRanking(NewSatisfactionConsumer(store)))

	if got := svc.ranking.behavioralScoresSnapshot(); got != nil {
		t.Fatalf("snapshot should be nil before first refresh, got %v", got)
	}
	if err := svc.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	scores := svc.ranking.behavioralScoresSnapshot()
	if scores["track|hello|adele"] != 3 || scores["track|noise|ugc"] != -2 {
		t.Errorf("published scores = %v, want hello=3 noise=-2", scores)
	}
}

func TestBehavioralRankingDisabled_SnapshotNil(t *testing.T) {
	store := &fakeSignalStore{signals: []ports.BehavioralSignal{{ResultSignature: "x", Score: 9}}}
	svc := NewService(nil, NewCircuitBreaker())
	svc.ranking.behavioralConsumer = NewSatisfactionConsumer(store)
	if got := svc.ranking.behavioralScoresSnapshot(); got != nil {
		t.Errorf("snapshot must be nil when behavioral ranking is off, got %v", got)
	}
}

func TestBehavioralScore_BreaksTiesOnly(t *testing.T) {
	a := scored{relevance: 1.0, behavioral: 2.0}
	b := scored{relevance: 1.0, behavioral: -1.0}
	if !rankLess(a, b) {
		t.Error("equal relevance: higher behavioral score should sort first")
	}

	hiRel := scored{relevance: 2.0, behavioral: -5.0}
	loRel := scored{relevance: 1.0, behavioral: 5.0}
	if !rankLess(hiRel, loRel) {
		t.Error("relevance must dominate behavioral; higher relevance sorts first")
	}

	z1 := scored{relevance: 1.0, pop: 5}
	z2 := scored{relevance: 1.0, pop: 1}
	if !rankLess(z1, z2) {
		t.Error("with zero behavioral, ordering must fall through to popularity")
	}
}

type mutableSignalStore struct {
	mu      sync.Mutex
	signals []ports.BehavioralSignal
	err     error
	calls   int
}

func (f *mutableSignalStore) SatisfactionSignals(context.Context, time.Time) ([]ports.BehavioralSignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.signals, f.err
}

func (f *mutableSignalStore) set(signals []ports.BehavioralSignal, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals, f.err = signals, err
}

func (f *mutableSignalStore) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRefreshBehavioralScores_NoConsumerIsNoop(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker())
	if err := svc.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatalf("no-consumer refresh must be a nil no-op, got %v", err)
	}
}

func TestRefreshBehavioralScores_ErrorKeepsLastSnapshot(t *testing.T) {
	store := &mutableSignalStore{signals: []ports.BehavioralSignal{{ResultSignature: "sig", Score: 2}}}
	svc := NewService(nil, NewCircuitBreaker(), WithBehavioralRanking(NewSatisfactionConsumer(store)))

	if err := svc.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.set(nil, errors.New("db down"))
	if err := svc.RefreshBehavioralScores(context.Background()); err == nil {
		t.Fatal("refresh must surface the consumer error to its caller")
	}
	if got := svc.ranking.behavioralScoresSnapshot(); got["sig"] != 2 {
		t.Errorf("snapshot after failed refresh = %v, want the last good map kept", got)
	}
}
