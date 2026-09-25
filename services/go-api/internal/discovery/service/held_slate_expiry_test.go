package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func expiringSlateService(t *testing.T, slates ...[]domain.SearchResult) (*Service, *driftingProvider, *fakeResultCache) {
	t.Helper()
	drifting := &driftingProvider{name: domain.ProviderDeezer, slates: slates}
	down := &countingProvider{name: domain.ProviderITunes, err: errors.New("upstream down")}
	heldCache := newFakeResultCache()
	svc := NewService(
		[]ports.SearchProvider{drifting, down},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
		WithHeldSlateCache(heldCache),
	)
	return svc, drifting, heldCache
}

func TestService_PagingSurvivesAHeldSlateExpiring(t *testing.T) {
	svc, drifting, heldCache := expiringSlateService(
		t,
		artistRun("Humble", "Alpha", 12),
		artistRun("Humble", "Bravo", 12),
	)
	userId := newUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	if drifting.calls != 1 {
		t.Fatalf("provider calls after page one = %d, want 1", drifting.calls)
	}

	heldCache.store = map[string][]domain.SearchResult{}

	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))
	if second.SearchId == first.SearchId {
		t.Fatal("a page served after the held slate expired must report a new search id")
	}
	if drifting.calls != 2 {
		t.Fatalf("provider calls after the expired page = %d, want 2 (one fresh fan-out)", drifting.calls)
	}

	third := searchPage(t, svc, userId, "humble", 10, 5, searchIdOf(t, second))
	if third.SearchId != second.SearchId {
		t.Fatalf("third page reports %q, want the ranking page two just re-established %q",
			third.SearchId, second.SearchId)
	}
	if drifting.calls != 2 {
		t.Fatalf("provider calls after page three = %d, want 2: the fresh ranking page two "+
			"produced must be held for page three too, not re-fanned-out", drifting.calls)
	}

	all := append(subtitles(second.Results), subtitles(third.Results)...)
	seen := map[string]bool{}
	for _, artist := range all {
		if seen[artist] {
			t.Fatalf("artist %q shown twice across pages two and three: %v", artist, all)
		}
		seen[artist] = true
	}
}
