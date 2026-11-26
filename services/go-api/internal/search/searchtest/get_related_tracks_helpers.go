package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/search/service/related"
	"testing"
)

type FakeRelatedProvider = memory.FakeRelatedProvider

func NewRelatedTracks(t testing.TB, providers map[string]ports.RelatedTracksProvider) *related.GetRelatedTracksService {
	t.Helper()
	return NewRelatedTracksWithBreaker(t, providers, providerguard.NewCircuitBreaker())
}

func NewRelatedTracksWithBreaker(t testing.TB, providers map[string]ports.RelatedTracksProvider, breaker *providerguard.CircuitBreaker) *related.GetRelatedTracksService {
	t.Helper()
	svc, err := related.NewGetRelatedTracksService(providers, breaker)
	if err != nil {
		t.Fatalf("related.NewGetRelatedTracksService: %v", err)
	}
	return svc
}
