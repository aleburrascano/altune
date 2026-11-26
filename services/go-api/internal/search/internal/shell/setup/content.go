package setup

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/service/artistpage"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/search/service/related"
	"altune/go-api/internal/shared/background"
)

func newAlbumService(t *providerTable, breaker *providerguard.CircuitBreaker) *artistpage.GetAlbumTracksService {
	deezer := t.named(rowDeezer)
	return artistpage.NewGetAlbumTracksService(
		t.albumProviders(),
		artistpage.WithTrackFeatured(deezer.trackFeatured),
		artistpage.WithAlbumFallbackSearcher(deezer.albumFallback),
		artistpage.WithAlbumCircuitBreaker(breaker),
	)
}

func newRelatedService(t *providerTable, breaker *providerguard.CircuitBreaker) *related.GetRelatedTracksService {
	return related.NewGetRelatedTracksService(t.relatedProviders(), related.WithRelatedCircuitBreaker(breaker))
}

func newArtistService(t *providerTable, s connections.Stores, breaker *providerguard.CircuitBreaker, bg *background.Tracker) *artistpage.GetArtistContentService {
	return artistpage.NewGetArtistContentService(t.artistProviders(),
		artistpage.WithContentCircuitBreaker(breaker),
		artistpage.WithContentEventStore(s.Events, bg),
		artistpage.WithMBAnchor(t.named(rowMusicBrainz).artistAnchor),
		artistpage.WithContentIdentityStore(s.Identities),
	)
}

func newFeaturedArtists(t *providerTable) *artistpage.FeaturedArtistResolver {
	return artistpage.NewFeaturedArtistResolver(t.named(rowMusicBrainz).featuredSearch, t.named(rowDeezer).trackFeatured)
}
