package service

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/ports"
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

	if got := svc.behavioralScoresSnapshot(); got != nil {
		t.Fatalf("snapshot should be nil before first refresh, got %v", got)
	}
	if err := svc.RefreshBehavioralScores(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	scores := svc.behavioralScoresSnapshot()
	if scores["track|hello|adele"] != 3 || scores["track|noise|ugc"] != -2 {
		t.Errorf("published scores = %v, want hello=3 noise=-2", scores)
	}
}

func TestBehavioralRankingDisabled_SnapshotNil(t *testing.T) {
	store := &fakeSignalStore{signals: []ports.BehavioralSignal{{ResultSignature: "x", Score: 9}}}
	// No WithBehavioralRanking → flag off → snapshot stays nil even if a consumer existed.
	svc := NewService(nil, NewCircuitBreaker())
	svc.behavioralConsumer = NewSatisfactionConsumer(store) // present but flag off
	if got := svc.behavioralScoresSnapshot(); got != nil {
		t.Errorf("snapshot must be nil when behavioral ranking is off, got %v", got)
	}
}

// TestBehavioralScore_BreaksTiesOnly proves the signal is a within-tie input:
// among equally relevant results it promotes the satisfied one, and it is inert
// when no scores are supplied (the default production path).
func TestBehavioralScore_BreaksTiesOnly(t *testing.T) {
	a := scored{relevance: 1.0, behavioral: 2.0}
	b := scored{relevance: 1.0, behavioral: -1.0}
	if !rankLess(a, b) {
		t.Error("equal relevance: higher behavioral score should sort first")
	}

	// Differing relevance dominates behavioral (behavioral only breaks ties).
	hiRel := scored{relevance: 2.0, behavioral: -5.0}
	loRel := scored{relevance: 1.0, behavioral: 5.0}
	if !rankLess(hiRel, loRel) {
		t.Error("relevance must dominate behavioral; higher relevance sorts first")
	}

	// Inert default: zero behavioral on both → falls through to later tiebreaks.
	z1 := scored{relevance: 1.0, pop: 5}
	z2 := scored{relevance: 1.0, pop: 1}
	if !rankLess(z1, z2) {
		t.Error("with zero behavioral, ordering must fall through to popularity")
	}
}
