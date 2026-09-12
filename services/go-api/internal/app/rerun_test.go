package app

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"

	discoveryPorts "altune/go-api/internal/discovery/ports"
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
