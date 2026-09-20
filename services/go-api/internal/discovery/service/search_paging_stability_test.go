package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// driftingProvider answers each fan-out with a different slate, the way a real
// provider does when its upstream re-ranks, a timeout truncates the answer, or
// a breaker drops a source between two pages of the same search.
type driftingProvider struct {
	name   domain.ProviderName
	slates [][]domain.SearchResult
	calls  int
}

func (p *driftingProvider) Name() domain.ProviderName { return p.name }

func (p *driftingProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	slate := p.slates[min(p.calls, len(p.slates)-1)]
	p.calls++
	return slate, nil
}

func (p *driftingProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

// artistRun is one provider answer: n tracks of the same title, told apart by
// artist, so a result that came from another fan-out is visible by its prefix.
func artistRun(title, prefix string, n int) []domain.SearchResult {
	out := make([]domain.SearchResult, n)
	for i := range out {
		out[i] = deezerTrack(title, fmt.Sprintf("%s %02d", prefix, i), float64(90-i))
	}
	return out
}

// pagingService searches one provider that drifts between fan-outs and one that
// is down. The failure keeps every slate partial, which is the slate the
// query-keyed cache never stores, so a later page has nothing to continue from
// but what the first page held.
func pagingService(t *testing.T, slates ...[]domain.SearchResult) (*Service, *driftingProvider) {
	t.Helper()
	drifting := &driftingProvider{name: domain.ProviderDeezer, slates: slates}
	down := &countingProvider{name: domain.ProviderITunes, err: errors.New("upstream down")}
	svc := NewService(
		[]ports.SearchProvider{drifting, down},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
	)
	return svc, drifting
}

func searchPage(
	t *testing.T,
	svc *Service,
	userId shared.UserId,
	raw string,
	offset, limit int,
	continues uuid.UUID,
) *SearchOutput {
	t.Helper()
	query, err := domain.NewPagedSearchQuery(raw, map[domain.ResultKind]bool{domain.ResultKindTrack: true}, limit, offset)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExecutePage(context.Background(), userId, query, false, continues)
	if err != nil {
		t.Fatalf("ExecutePage(offset=%d): %v", offset, err)
	}
	return out
}

func searchIdOf(t *testing.T, out *SearchOutput) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(out.SearchId)
	if err != nil {
		t.Fatalf("SearchId %q is not a uuid: %v", out.SearchId, err)
	}
	return id
}

func TestService_PagesOfOneSearchComeFromOneRanking(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Humble", "Bravo", 12))
	reference, _ := pagingService(t, artistRun("Humble", "Alpha", 12))
	userId := newUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))

	paged := append(subtitles(first.Results), subtitles(second.Results)...)
	unpaged := subtitles(searchPage(t, reference, newUser(), "humble", 0, 10, uuid.Nil).Results)
	if len(paged) != len(unpaged) {
		t.Fatalf("paged through %d results, want the %d of one ranking: %v", len(paged), len(unpaged), paged)
	}
	for i, artist := range unpaged {
		if paged[i] != artist {
			t.Fatalf("position %d = %q, want %q (paged %v, one ranking %v)", i, paged[i], artist, paged, unpaged)
		}
	}
	if second.SearchId != first.SearchId {
		t.Errorf("second page reports search %q, want the continued %q", second.SearchId, first.SearchId)
	}
	if second.Total != first.Total {
		t.Errorf("total = %d on page two, %d on page one", second.Total, first.Total)
	}
}

func TestService_PageAskedForWithoutASearchIdRanksAfresh(t *testing.T) {
	svc, drifting := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Humble", "Bravo", 12))
	userId := newUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	second := searchPage(t, svc, userId, "humble", 5, 5, uuid.Nil)

	if drifting.calls != 2 {
		t.Errorf("provider calls = %d, want 2: a page naming no search fans out as before", drifting.calls)
	}
	if len(second.Results) != 5 {
		t.Fatalf("page two served %d results, want 5: %v", len(second.Results), subtitles(second.Results))
	}
	if second.SearchId == first.SearchId {
		t.Error("a page cut from a fresh ranking must report a new search id")
	}
}

func TestService_SearchIdReplayedAgainstAnotherQueryServesThatQuery(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Loyalty", "Bravo", 12))
	userId := newUser()

	humble := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	loyalty := searchPage(t, svc, userId, "loyalty", 0, 5, searchIdOf(t, humble))

	if len(loyalty.Results) == 0 {
		t.Fatal("the borrowed id served nothing, so this proves nothing about which slate it read")
	}
	for _, artist := range subtitles(loyalty.Results) {
		if !strings.HasPrefix(artist, "Bravo") {
			t.Fatalf("a search id replayed against another query served %v, want that query's own fan-out",
				subtitles(loyalty.Results))
		}
	}
	if loyalty.SearchId == humble.SearchId {
		t.Error("a query that borrowed another search's id must report its own")
	}
}
