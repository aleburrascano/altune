package memory

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
)

func NewRelatedTracks(t TB, providers map[string]ports.RelatedTracksProvider) *service.GetRelatedTracksService {
	t.Helper()
	return NewRelatedTracksWithBreaker(t, providers, service.NewCircuitBreaker(clock.Now))
}

func NewRelatedTracksWithBreaker(t TB, providers map[string]ports.RelatedTracksProvider, breaker *service.CircuitBreaker) *service.GetRelatedTracksService {
	t.Helper()
	svc, err := service.NewGetRelatedTracksService(providers, breaker)
	if err != nil {
		t.Fatalf("related.NewGetRelatedTracksService: %v", err)
	}
	return svc
}
