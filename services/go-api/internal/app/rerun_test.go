package app

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"

	"github.com/go-chi/chi/v5"
)

type fakeSearchProvider struct {
	name    domain.ProviderName
	results []domain.SearchResult
	panics  bool
}

func (f fakeSearchProvider) Name() domain.ProviderName { return f.name }

func (f fakeSearchProvider) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if f.panics {
		panic("boom: malformed provider response")
	}
	return f.results, nil
}

func (f fakeSearchProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

// stalledSearchProvider stands in for an upstream that accepted the request and
// went quiet: it returns only when its context ends, and asks for a per-provider
// budget far longer than any inspector should wait.
type stalledSearchProvider struct{}

func (stalledSearchProvider) Name() domain.ProviderName { return domain.ProviderDeezer }

func (stalledSearchProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalledSearchProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindArtist: true, domain.ResultKindTrack: true}
}

func (stalledSearchProvider) SearchTimeout() time.Duration { return time.Minute }

// stalledTransport is the same stall one layer down, for the adapters reRun
// builds itself: the response never comes, only the context ends it.
type stalledTransport struct{}

func (stalledTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// TestAdminInspectors_returnWithinBudgetWhenAProviderStalls reproduces #1996:
// reRun and inspectSearch ran on the raw request context, so a provider that
// stalls held the admin request — and one of the two in-flight inspector slots
// — open for as long as that provider's own client allowed. Before the wiring
// took a budget, both routes sit here until the adapters' 10-15s clients give
// up, well past the window below.
func TestAdminInspectors_returnWithinBudgetWhenAProviderStalls(t *testing.T) {
	const budget = 200 * time.Millisecond
	// A stalled inspector is a hung one; this window only has to be short
	// enough to exclude the unbudgeted provider clients.
	const wait = 5 * time.Second

	searchSvc := discoveryService.NewService(
		[]discoveryPorts.SearchProvider{stalledSearchProvider{}},
		discoveryService.NewCircuitBreaker(),
	)
	artistSvc := discoveryService.NewGetArtistContentService(map[domain.ProviderName]discoveryPorts.ArtistContentProvider{})
	h := withAdminInspectors(adminHandler.New(nil, nil), &config.Config{}, stalledTransport{}, searchSvc, artistSvc, budget)
	r := chi.NewRouter()
	h.RegisterData(r)

	for _, path := range []string{"/rerun", "/search"} {
		t.Run(path, func(t *testing.T) {
			answered := make(chan int, 1)
			go func() {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query":"kendrick"}`)))
				answered <- rec.Code
			}()
			select {
			case <-answered:
			case <-time.After(wait):
				t.Fatalf("%s did not answer within %s: the inspector has no wall-time budget of its own", path, wait)
			}
		})
	}
}

func TestFanOutRerun_recoversPanicAsProviderError(t *testing.T) {
	provs := []discoveryPorts.SearchProvider{
		fakeSearchProvider{name: domain.ProviderDeezer, panics: true},
		fakeSearchProvider{name: domain.ProviderSpotify, results: []domain.SearchResult{{Title: "Survivor"}}},
	}

	perProvider, traces := fanOutRerun(context.Background(), provs, "q", map[domain.ResultKind]bool{domain.ResultKindTrack: true})

	if len(traces) != 2 {
		t.Fatalf("want 2 provider traces, got %d", len(traces))
	}

	panicked := traces[0]
	if panicked.Status != domain.ProviderStatusError.String() {
		t.Errorf("panicking provider: want status %q, got %q", domain.ProviderStatusError.String(), panicked.Status)
	}
	if panicked.Err == "" {
		t.Error("panicking provider: want non-empty error surfaced from the recovered panic")
	}

	survivor := traces[1]
	if survivor.Status != domain.ProviderStatusOK.String() {
		t.Errorf("other provider: want status %q, got %q", domain.ProviderStatusOK.String(), survivor.Status)
	}
	if len(perProvider[1]) != 1 || perProvider[1][0].Title != "Survivor" {
		t.Errorf("other provider: want its results to survive, got %+v", perProvider[1])
	}
}
