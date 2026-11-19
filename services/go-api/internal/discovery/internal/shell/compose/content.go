package compose

import (
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/service/content"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/related"
)

func newAlbumService(t *providerTable, breaker *providercall.CircuitBreaker) *content.GetAlbumTracksService {
	deezer := t.named(rowDeezer)
	return content.NewGetAlbumTracksService(
		t.albumProviders(),
		content.WithTrackFeatured(deezer.trackFeatured),
		content.WithAlbumFallbackSearcher(deezer.albumFallback),
		content.WithAlbumCircuitBreaker(breaker),
	)
}

func newRelatedService(t *providerTable, breaker *providercall.CircuitBreaker) *related.GetRelatedTracksService {
	return related.NewGetRelatedTracksService(t.relatedProviders(), related.WithRelatedCircuitBreaker(breaker))
}

func newArtistService(t *providerTable, s handles.Stores, breaker *providercall.CircuitBreaker) *content.GetArtistContentService {
	opts := []content.ArtistContentOption{
		content.WithContentCircuitBreaker(breaker),
		content.WithContentEventStore(s.Events),
		content.WithMBAnchor(t.named(rowMusicBrainz).artistAnchor),
	}
	if s.Pool != nil {
		opts = append(opts, content.WithContentIdentityStore(s.IdentityStore()))
	}
	return content.NewGetArtistContentService(t.artistProviders(), opts...)
}

func newFeaturedArtists(t *providerTable) *content.FeaturedArtistResolver {
	return content.NewFeaturedArtistResolver(t.named(rowMusicBrainz).featuredSearch, t.named(rowDeezer).trackFeatured)
}
