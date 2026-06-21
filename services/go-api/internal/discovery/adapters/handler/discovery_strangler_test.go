package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"altune/go-api/internal/auth"
	discdomain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	discovery2 "altune/go-api/internal/discovery2/service"
	"altune/go-api/internal/shared"

	"github.com/go-chi/chi/v5"
)

// The rebuilt pipeline's skeleton must satisfy the handler's search seam, so the
// strangler switch stays type-safe as discovery2 grows (plan 003).
var _ newSearchPipeline = (*discovery2.Service)(nil)

// fakeNewSearchPipeline is a stand-in for the rebuilt pipeline. It records that
// it ran and returns a recognizable result so a test can tell which pipeline
// served the request.
type fakeNewSearchPipeline struct {
	called bool
	output *service.SearchOutput
	err    error
}

func (f *fakeNewSearchPipeline) Execute(
	_ context.Context,
	_ shared.UserId,
	_ *discdomain.SearchQuery,
	_ bool,
) (*service.SearchOutput, error) {
	f.called = true
	return f.output, f.err
}

// buildStranglerRouter wires a handler whose legacy pipeline returns a result
// titled "from-legacy", optionally overlaid with a new-pipeline seam.
func buildStranglerRouter(t *testing.T, newSearch *fakeNewSearchPipeline) chi.Router {
	t.Helper()

	legacyProvider := &fakeSearchProvider{
		name: discdomain.ProviderDeezer,
		results: []discdomain.SearchResult{
			{
				Kind:       discdomain.ResultKindTrack,
				Title:      "from-legacy",
				Confidence: discdomain.ConfidenceLow,
				Sources: []discdomain.SourceRef{
					{Provider: discdomain.ProviderDeezer, ExternalID: "1", URL: "https://deezer.com/1"},
				},
			},
		},
	}
	cb := service.NewCircuitBreaker()
	searchSvc := service.NewSearchMusicService(
		[]ports.SearchProvider{legacyProvider}, nil, &fakeSearchHistoryRepo{}, cb,
	)

	var opts []Option
	if newSearch != nil {
		opts = append(opts, WithNewSearchPipeline(newSearch))
	}
	h := NewDiscoveryHandler(searchSvc, nil, nil, nil, nil, nil, nil, opts...)

	r := chi.NewRouter()
	r.Use(auth.Middleware(&discFakeTokenVerifier{userId: discTestUserId}))
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleSearch_RoutesToNewPipelineWhenWired(t *testing.T) {
	newSearch := &fakeNewSearchPipeline{
		output: &service.SearchOutput{
			Results: []discdomain.SearchResult{
				{
					Kind:       discdomain.ResultKindTrack,
					Title:      "from-new-pipeline",
					Confidence: discdomain.ConfidenceLow,
					Sources: []discdomain.SourceRef{
						{Provider: discdomain.ProviderDeezer, ExternalID: "9", URL: "https://deezer.com/9"},
					},
				},
			},
		},
	}
	router := buildStranglerRouter(t, newSearch)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=anything", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if !newSearch.called {
		t.Fatal("expected the new pipeline to be invoked, but it was not")
	}
	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if len(resp.Results) != 1 || resp.Results[0].Title != "from-new-pipeline" {
		t.Errorf("expected new-pipeline result, got %+v", resp.Results)
	}
}

func TestHandleSearch_UsesLegacyPipelineWhenSeamUnset(t *testing.T) {
	// With no new pipeline wired, executeSearch has only one branch: the legacy
	// searchSvc. A successful 200 with a well-formed response is therefore proof
	// the legacy path served the request. (Result content is left to the legacy
	// pipeline's own tests — it runs real relevance ranking, which this seam test
	// must not couple to.)
	router := buildStranglerRouter(t, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=anything", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var resp DiscoverySearchResponse
	discDecodeJSON(t, rec, &resp)
	if resp.Results == nil {
		t.Error("expected a non-nil results array from the legacy pipeline")
	}
}

func TestHandleSearch_NewPipelineErrorReturns500(t *testing.T) {
	newSearch := &fakeNewSearchPipeline{err: errors.New("boom")}
	router := buildStranglerRouter(t, newSearch)

	rec := discServe(t, router, http.MethodGet, "/discovery/search?q=anything", nil)

	discAssertStatus(t, rec, http.StatusInternalServerError)
	if !newSearch.called {
		t.Fatal("expected the new pipeline to be invoked")
	}
}
