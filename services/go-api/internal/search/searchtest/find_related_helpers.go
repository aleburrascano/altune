package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/search/internal/service/related"
	"altune/go-api/internal/shared/clock"
	"testing"
)

type (
	CountingRelationshipQuerier = memory.CountingRelationshipQuerier
	CountingAlbumTracksProvider = memory.CountingAlbumTracksProvider
)

type FindRelatedPart func(*findRelatedParts)

type findRelatedParts struct {
	sources []func(testing.TB, *providerguard.CircuitBreaker) related.LookupSource
	breaker *providerguard.CircuitBreaker
}

func RelatedLibrary(querier ports.RelationshipQuerier) FindRelatedPart {
	return func(p *findRelatedParts) {
		p.sources = append(p.sources, func(t testing.TB, _ *providerguard.CircuitBreaker) related.LookupSource {
			t.Helper()
			source, err := related.LibraryMatches(querier)
			if err != nil {
				t.Fatalf("related.LibraryMatches: %v", err)
			}
			return source
		})
	}
}

func RelatedAlbumTracks(provider ports.AlbumContentProvider) FindRelatedPart {
	return func(p *findRelatedParts) {
		p.sources = append(p.sources, func(t testing.TB, breaker *providerguard.CircuitBreaker) related.LookupSource {
			t.Helper()
			source, err := related.AlbumTracksSource(provider, breaker)
			if err != nil {
				t.Fatalf("related.AlbumTracksSource: %v", err)
			}
			return source
		})
	}
}

func RelatedArtistAlbums(provider ports.ArtistContentProvider) FindRelatedPart {
	return func(p *findRelatedParts) {
		p.sources = append(p.sources, func(t testing.TB, breaker *providerguard.CircuitBreaker) related.LookupSource {
			t.Helper()
			source, err := related.ArtistAlbumsSource(provider, breaker)
			if err != nil {
				t.Fatalf("related.ArtistAlbumsSource: %v", err)
			}
			return source
		})
	}
}

func RelatedBreaker(breaker *providerguard.CircuitBreaker) FindRelatedPart {
	return func(p *findRelatedParts) { p.breaker = breaker }
}

func NewFindRelated(t testing.TB, parts ...FindRelatedPart) *related.FindRelatedService {
	t.Helper()
	p := findRelatedParts{breaker: providerguard.NewCircuitBreaker(clock.Now)}
	for _, part := range parts {
		part(&p)
	}
	sources := make([]related.LookupSource, 0, len(p.sources))
	for _, build := range p.sources {
		sources = append(sources, build(t, p.breaker))
	}
	svc, err := related.NewFindRelatedService(clock.Now, sources...)
	if err != nil {
		t.Fatalf("related.NewFindRelatedService: %v", err)
	}
	return svc
}
