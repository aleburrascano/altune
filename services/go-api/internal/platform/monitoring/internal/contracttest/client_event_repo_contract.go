package contracttest

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"altune/go-api/internal/shared/ids"
	"testing"
	"time"

	"github.com/google/uuid"
)

type ClientEventRepoUnderTest interface {
	ports.ClientEventStore
	NewUser(t *testing.T) ids.UserId
	RegisterIdentity(t *testing.T, user ids.UserId)
	Count(t *testing.T, user ids.UserId) int
}

func ClientEventRepoContract(t *testing.T, newRepo func(t *testing.T) ClientEventRepoUnderTest) {
	t.Helper()
	contracts := map[string]func(*testing.T, ClientEventRepoUnderTest){
		"a repeated event id for one user is stored once":    contractRepeatedEventIDIsStoredOnce,
		"events without an id are all kept":                  contractEventsWithoutAnIDAreAllKept,
		"the same event id for two users is stored for each": contractEventIDIsScopedToTheUser,
		"prune removes only rows older than the retention":   contractPruneKeepsRowsInsideTheRetention,
		"erase removes only the rows of deleted identities":  contractEraseKeepsTheRowsOfLiveIdentities,
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newRepo(t)) })
	}
}

func appendOrFail(t *testing.T, r ClientEventRepoUnderTest, event domain.ClientEvent) {
	t.Helper()
	if err := r.Append(t.Context(), event); err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func contractRepeatedEventIDIsStoredOnce(t *testing.T, r ClientEventRepoUnderTest) {
	user := r.NewUser(t)
	event := domain.ClientEvent{UserId: user, Type: domain.ClientEventClientError, EventId: uuid.NewString(), OccurredAt: time.Now()}

	for range 2 {
		appendOrFail(t, r, event)
	}

	if got := r.Count(t, user); got != 1 {
		t.Fatalf("rows after a repeated event id = %d, want 1", got)
	}
}

func contractEventsWithoutAnIDAreAllKept(t *testing.T, r ClientEventRepoUnderTest) {
	user := r.NewUser(t)
	event := domain.ClientEvent{UserId: user, Type: domain.ClientEventScreenView, OccurredAt: time.Now()}

	for range 3 {
		appendOrFail(t, r, event)
	}

	if got := r.Count(t, user); got != 3 {
		t.Fatalf("rows = %d, want 3: events with no event id are never deduplicated", got)
	}
}

func contractEventIDIsScopedToTheUser(t *testing.T, r ClientEventRepoUnderTest) {
	first, second := r.NewUser(t), r.NewUser(t)
	id := uuid.NewString()

	appendOrFail(t, r, domain.ClientEvent{UserId: first, Type: domain.ClientEventClientError, EventId: id, OccurredAt: time.Now()})
	appendOrFail(t, r, domain.ClientEvent{UserId: second, Type: domain.ClientEventClientError, EventId: id, OccurredAt: time.Now()})

	if r.Count(t, first) != 1 || r.Count(t, second) != 1 {
		t.Fatalf("rows first=%d second=%d, want 1 and 1", r.Count(t, first), r.Count(t, second))
	}
}

func contractPruneKeepsRowsInsideTheRetention(t *testing.T, r ClientEventRepoUnderTest) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	stale, fresh := r.NewUser(t), r.NewUser(t)
	appendOrFail(t, r, domain.ClientEvent{UserId: stale, Type: domain.ClientEventScreenView, OccurredAt: now.Add(-domain.ClientEventRetention - time.Minute)})
	appendOrFail(t, r, domain.ClientEvent{UserId: fresh, Type: domain.ClientEventScreenView, OccurredAt: now.Add(-domain.ClientEventRetention + time.Minute)})

	pruned, err := r.Prune(t.Context(), now)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if r.Count(t, stale) != 0 || r.Count(t, fresh) != 1 {
		t.Errorf("rows stale=%d fresh=%d, want 0 and 1", r.Count(t, stale), r.Count(t, fresh))
	}
	if pruned < 1 {
		t.Errorf("pruned = %d, want at least the stale row", pruned)
	}
}

func contractEraseKeepsTheRowsOfLiveIdentities(t *testing.T, r ClientEventRepoUnderTest) {
	kept, gone := r.NewUser(t), r.NewUser(t)
	for _, user := range []ids.UserId{kept, gone} {
		appendOrFail(t, r, domain.ClientEvent{UserId: user, Type: domain.ClientEventScreenView, OccurredAt: time.Now()})
	}
	r.RegisterIdentity(t, kept)

	erased, err := r.EraseRowsOfDeletedIdentities(t.Context())
	if err != nil {
		t.Fatalf("EraseRowsOfDeletedIdentities: %v", err)
	}

	if r.Count(t, gone) != 0 || r.Count(t, kept) != 1 {
		t.Errorf("rows kept=%d gone=%d, want 1 and 0", r.Count(t, kept), r.Count(t, gone))
	}
	if erased < 1 {
		t.Errorf("erased = %d, want at least the deleted identity's row", erased)
	}
}
