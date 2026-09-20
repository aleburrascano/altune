package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"context"
	"net/http"
	"testing"

	discdomain "altune/go-api/internal/discovery/domain"

	"github.com/go-chi/chi/v5"
)

// driftingSearchProvider answers each fan-out with the next slate, so a second
// page that re-ranks instead of continuing shows results the first page never
// ranked.
type driftingSearchProvider struct {
	slates [][]discdomain.SearchResult
	calls  int
}

func (p *driftingSearchProvider) Name() discdomain.ProviderName { return discdomain.ProviderDeezer }

func (p *driftingSearchProvider) Search(_ context.Context, _ string, _ map[discdomain.ResultKind]bool) ([]discdomain.SearchResult, error) {
	slate := p.slates[min(p.calls, len(p.slates)-1)]
	p.calls++
	return slate, nil
}

func (p *driftingSearchProvider) SupportedKinds() map[discdomain.ResultKind]bool {
	return map[discdomain.ResultKind]bool{discdomain.ResultKindTrack: true}
}

type mapResultCache struct {
	entries map[string][]discdomain.SearchResult
}

func (c *mapResultCache) Get(_ context.Context, key string) ([]discdomain.SearchResult, bool) {
	results, hit := c.entries[key]
	return results, hit
}

func (c *mapResultCache) Set(_ context.Context, key string, results []discdomain.SearchResult) {
	c.entries[key] = results
}

func searchSlate(prefix string, n int) []discdomain.SearchResult {
	out := make([]discdomain.SearchResult, n)
	for i := range out {
		out[i] = discdomain.NewProviderResult(
			discdomain.ResultKindTrack, "Humble", prefix+string(rune('a'+i)), "",
			discdomain.SourceRef{Provider: discdomain.ProviderDeezer, ExternalID: prefix + string(rune('a'+i))}, nil)
	}
	return out
}

// pagingRouter serves search over a provider that drifts between fan-outs and
// one that is down. The failure keeps every slate partial, the slate the
// query-keyed cache never stores, so a later page has nothing to continue from
// but what the first page held.
func pagingRouter(provider ports.SearchProvider) chi.Router {
	down := &fakeSearchProvider{name: discdomain.ProviderITunes, err: context.DeadlineExceeded}
	searchSvc := service.NewService(
		[]ports.SearchProvider{provider, down},
		service.NewCircuitBreaker(),
		service.WithResultCache(&mapResultCache{entries: map[string][]discdomain.SearchResult{}}),
	)
	h := NewDiscoveryHandler(DiscoveryServices{Search: searchSvc})
	r := chi.NewRouter()
	r.Use(auth.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func searchPageOverHTTP(t *testing.T, router chi.Router, path string) DiscoverySearchResponse {
	t.Helper()
	rec := discServe(t, router, http.MethodGet, path, nil)
	discAssertStatus(t, rec, http.StatusOK)
	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	return resp
}

func TestHandleSearch_SearchIdKeepsTheNextPageInTheSameRanking(t *testing.T) {
	router := pagingRouter(&driftingSearchProvider{
		slates: [][]discdomain.SearchResult{searchSlate("first-", 12), searchSlate("second-", 12)},
	})

	first := searchPageOverHTTP(t, router, "/discovery/search?q=humble&kinds=track&limit=5")
	second := searchPageOverHTTP(t, router,
		"/discovery/search?q=humble&kinds=track&limit=5&offset=5&search_id="+first.SearchID)

	seen := map[string]bool{}
	for _, r := range append(first.Results, second.Results...) {
		if seen[r.ResultSignature] {
			t.Fatalf("result %q served on both pages", r.Subtitle)
		}
		seen[r.ResultSignature] = true
	}
	if len(seen) != 10 {
		t.Fatalf("two pages of 5 served %d distinct results", len(seen))
	}
	for _, r := range second.Results {
		if r.Sources[0].ExternalID[:6] != "first-" {
			t.Fatalf("page two served %q, which the first page's ranking never held", r.Sources[0].ExternalID)
		}
	}
	if second.SearchID != first.SearchID {
		t.Errorf("search_id = %q on page two, want the continued %q", second.SearchID, first.SearchID)
	}
	if second.Total != first.Total {
		t.Errorf("total = %d on page two, %d on page one", second.Total, first.Total)
	}
}
