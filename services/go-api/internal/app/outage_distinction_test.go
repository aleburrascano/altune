package app

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"testing"

	discoveryService "altune/go-api/internal/discovery/service"
)

// outageProvider is a search provider whose fan-out outcome the test controls:
// a non-nil err simulates a provider that is down, while an empty result with
// nil err simulates a provider that is healthy but has no match.
type outageProvider struct {
	name    domain.ProviderName
	results []domain.SearchResult
	err     error
}

func (p outageProvider) Name() domain.ProviderName { return p.name }

func (p outageProvider) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return p.results, p.err
}

func (p outageProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindArtist: true}
}

func inspectorForProvider(p ports.SearchProvider) *searchInspector {
	svc := discoveryService.NewService([]ports.SearchProvider{p}, discoveryService.NewCircuitBreaker())
	return &searchInspector{svc: svc}
}

func detailReRunnerForProvider(p ports.SearchProvider) *detailReRunner {
	svc := discoveryService.NewService([]ports.SearchProvider{p}, discoveryService.NewCircuitBreaker())
	return &detailReRunner{searchSvc: svc}
}

// TestInspectSearch_outageIsDistinguishableFromEmpty pins the reported gap: a
// total provider outage and a genuine zero-result query both yield an empty
// result set, so the inspector must signal the outage with an error instead of
// reporting the same empty, nil-error shape for both.
func TestInspectSearch_outageIsDistinguishableFromEmpty(t *testing.T) {
	down := inspectorForProvider(outageProvider{name: domain.ProviderDeezer, err: errors.New("provider unreachable")})
	outageRows, outageErr := down.InspectSearch(context.Background(), "kendrick", nil)

	empty := inspectorForProvider(outageProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{}})
	emptyRows, emptyErr := empty.InspectSearch(context.Background(), "kendrick", nil)

	if len(outageRows) != 0 || len(emptyRows) != 0 {
		t.Fatalf("want both shapes empty, got outage=%d empty=%d rows", len(outageRows), len(emptyRows))
	}
	if emptyErr != nil {
		t.Fatalf("genuine no-match must not error, got %v", emptyErr)
	}
	if !errors.Is(outageErr, discoveryService.ErrAllProvidersFailed) {
		t.Fatalf("all-providers-down must surface ErrAllProvidersFailed, got %v (indistinguishable from no-match)", outageErr)
	}
}

// TestResolveTopArtist_outageIsDistinguishableFromEmpty does the same for
// ReRunDetail's artist-resolution step, which otherwise returns (empty, false,
// nil) for both an outage and a genuine no-match.
func TestResolveTopArtist_outageIsDistinguishableFromEmpty(t *testing.T) {
	down := detailReRunnerForProvider(outageProvider{name: domain.ProviderDeezer, err: errors.New("provider unreachable")})
	_, outageOK, outageErr := down.resolveTopArtist(context.Background(), "kendrick")

	empty := detailReRunnerForProvider(outageProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{}})
	_, emptyOK, emptyErr := empty.resolveTopArtist(context.Background(), "kendrick")

	if outageOK || emptyOK {
		t.Fatalf("want no artist resolved in either case, got outage=%v empty=%v", outageOK, emptyOK)
	}
	if emptyErr != nil {
		t.Fatalf("genuine no-match must not error, got %v", emptyErr)
	}
	if !errors.Is(outageErr, discoveryService.ErrAllProvidersFailed) {
		t.Fatalf("all-providers-down must surface ErrAllProvidersFailed, got %v (indistinguishable from no-match)", outageErr)
	}
}
