package searchtest

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"testing"
)

type artistContentParts struct {
	identities ports.IdentityStore
	anchor     ports.MusicBrainzDiscographyAnchor
	events     ports.EventStore
	tracker    *background.Tracker
	breaker    *providerguard.CircuitBreaker
}

type ArtistContentPart func(*artistContentParts)

func ArtistIdentities(store ports.IdentityStore) ArtistContentPart {
	return func(p *artistContentParts) { p.identities = store }
}

func ArtistAnchor(anchor ports.MusicBrainzDiscographyAnchor) ArtistContentPart {
	return func(p *artistContentParts) { p.anchor = anchor }
}

func ArtistEvents(store ports.EventStore, tracker *background.Tracker) ArtistContentPart {
	return func(p *artistContentParts) { p.events, p.tracker = store, tracker }
}

func ArtistBreaker(breaker *providerguard.CircuitBreaker) ArtistContentPart {
	return func(p *artistContentParts) { p.breaker = breaker }
}

func NewArtistContent(t testing.TB, providers map[domain.ProviderName]ports.ArtistContentProvider, parts ...ArtistContentPart) *artistpage.GetArtistContentService {
	t.Helper()
	p := artistContentParts{breaker: providerguard.NewCircuitBreaker(clock.Now)}
	for _, part := range parts {
		part(&p)
	}
	var identity artistpage.ArtistIdentity = artistpage.SeedPathIdentity{}
	if p.identities != nil {
		store, err := artistpage.NewStoreIdentity(p.identities)
		if err != nil {
			t.Fatalf("artistpage.NewStoreIdentity: %v", err)
		}
		identity = store
	}
	var anchoring artistpage.Anchoring = artistpage.NoAnchoring{}
	if p.anchor != nil {
		musicBrainz, err := artistpage.NewMusicBrainzAnchoring(p.anchor, p.breaker)
		if err != nil {
			t.Fatalf("artistpage.NewMusicBrainzAnchoring: %v", err)
		}
		anchoring = musicBrainz
	}
	var observer artistpage.DiscographyObserver = artistpage.NoDiscographyObserver{}
	if p.events != nil {
		telemetry, err := artistpage.NewDiscographyTelemetry(p.events, p.tracker, clock.Now)
		if err != nil {
			t.Fatalf("artistpage.NewDiscographyTelemetry: %v", err)
		}
		observer = telemetry
	}
	svc, err := artistpage.NewGetArtistContentService(providers, identity, anchoring, observer, p.breaker)
	if err != nil {
		t.Fatalf("artistpage.NewGetArtistContentService: %v", err)
	}
	return svc
}

type albumTracksParts struct {
	featured artistpage.DeezerFeaturedLookup
	fallback ports.SearchProvider
	breaker  *providerguard.CircuitBreaker
}

type AlbumTracksPart func(*albumTracksParts)

func AlbumFeatured(lookup artistpage.DeezerFeaturedLookup) AlbumTracksPart {
	return func(p *albumTracksParts) { p.featured = lookup }
}

func AlbumFallbackSearcher(searcher ports.SearchProvider) AlbumTracksPart {
	return func(p *albumTracksParts) { p.fallback = searcher }
}

func AlbumBreaker(breaker *providerguard.CircuitBreaker) AlbumTracksPart {
	return func(p *albumTracksParts) { p.breaker = breaker }
}

func NewAlbumTracks(t testing.TB, providers map[domain.ProviderName]ports.AlbumContentProvider, parts ...AlbumTracksPart) *artistpage.GetAlbumTracksService {
	t.Helper()
	p := albumTracksParts{breaker: providerguard.NewCircuitBreaker(clock.Now)}
	for _, part := range parts {
		part(&p)
	}
	var featured artistpage.FeaturedEnrichment = artistpage.NoFeaturedEnrichment{}
	if p.featured != nil {
		deezer, err := artistpage.NewDeezerFeaturedEnrichment(p.featured, p.breaker)
		if err != nil {
			t.Fatalf("artistpage.NewDeezerFeaturedEnrichment: %v", err)
		}
		featured = deezer
	}
	var fallback artistpage.AlbumFallback = artistpage.NoAlbumFallback{}
	if p.fallback != nil {
		search, err := artistpage.NewSearchAlbumFallback(p.fallback, p.breaker)
		if err != nil {
			t.Fatalf("artistpage.NewSearchAlbumFallback: %v", err)
		}
		fallback = search
	}
	svc, err := artistpage.NewGetAlbumTracksService(providers, featured, fallback, p.breaker)
	if err != nil {
		t.Fatalf("artistpage.NewGetAlbumTracksService: %v", err)
	}
	return svc
}

func NewFeaturedResolver(t testing.TB, mb artistpage.MBFeaturedSearcher, deezer artistpage.DeezerFeaturedLookup) *artistpage.FeaturedArtistResolver {
	t.Helper()
	resolver, err := artistpage.NewFeaturedArtistResolver(mb, deezer)
	if err != nil {
		t.Fatalf("artistpage.NewFeaturedArtistResolver: %v", err)
	}
	return resolver
}
