package app

import (
	"altune/go-api/internal/shared"
	"context"
	"sync"
	"testing"

	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/google/uuid"
)

type recordingEventStore struct {
	mu     sync.Mutex
	events []domain.InteractionEvent
}

func (r *recordingEventStore) Append(_ context.Context, e domain.InteractionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingEventStore) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type stubSearchProvider struct{}

func (stubSearchProvider) Name() domain.ProviderName { return domain.ProviderDeezer }

func (stubSearchProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, nil
}

func (stubSearchProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func runSmokeEvalRecording(t *testing.T, user shared.UserId) int {
	t.Helper()
	store := &recordingEventStore{}
	svc := discoveryService.NewService(
		[]discoveryPorts.SearchProvider{stubSearchProvider{}},
		discoveryService.NewCircuitBreaker(),
		discoveryService.WithEventStore(store),
	)
	if _, err := runSmokeEval(context.Background(), svc, user); err != nil {
		t.Fatalf("runSmokeEval: %v", err)
	}
	svc.WaitForBackground()
	return store.count()
}

// The smoke eval runs real per-user search paths, so running it as a real
// account persists InteractionEvents under that id. It must instead run under
// the synthetic system identity, which the search service refuses to record.
func TestSmokeEval_RunsUnderSyntheticIdentity(t *testing.T) {
	if !evalUserId().IsSystem() {
		t.Fatal("smoke eval must run under the synthetic system identity")
	}

	// Control: a real operator account would get an event per query (contamination).
	if n := runSmokeEvalRecording(t, shared.NewUserId(uuid.New())); n == 0 {
		t.Fatal("real identity should persist InteractionEvents (control)")
	}

	// Fix: the identity the runner actually uses persists nothing.
	if n := runSmokeEvalRecording(t, evalUserId()); n != 0 {
		t.Fatalf("smoke eval persisted %d InteractionEvents under its identity; want 0", n)
	}
}
