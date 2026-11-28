package searchtest

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"testing"
)

type artistContentParts struct {
	identities ports.IdentityStore
	anchor     ports.MusicBrainzDiscographyAnchor
	events     ports.EventStore
	tracker    *background.Tracker
	breaker    *service.CircuitBreaker
	tracklists map[domain.ProviderName]ports.AlbumContentProvider
}

type ArtistContentPart func(*artistContentParts)

func ArtistTracklists(tracklists map[domain.ProviderName]ports.AlbumContentProvider) ArtistContentPart {
	return func(p *artistContentParts) { p.tracklists = tracklists }
}

func ArtistIdentities(store ports.IdentityStore) ArtistContentPart {
	return func(p *artistContentParts) { p.identities = store }
}

func ArtistAnchor(anchor ports.MusicBrainzDiscographyAnchor) ArtistContentPart {
	return func(p *artistContentParts) { p.anchor = anchor }
}

func ArtistEvents(store ports.EventStore, tracker *background.Tracker) ArtistContentPart {
	return func(p *artistContentParts) { p.events, p.tracker = store, tracker }
}

func ArtistBreaker(breaker *service.CircuitBreaker) ArtistContentPart {
	return func(p *artistContentParts) { p.breaker = breaker }
}

func NewArtistContent(t testing.TB, providers map[domain.ProviderName]ports.ArtistContentProvider, parts ...ArtistContentPart) *service.GetArtistContentService {
	t.Helper()
	p := artistContentParts{breaker: service.NewCircuitBreaker(clock.Now), tracklists: map[domain.ProviderName]ports.AlbumContentProvider{}}
	for _, part := range parts {
		part(&p)
	}
	var identity service.ArtistIdentity = service.SeedPathIdentity{}
	if p.identities != nil {
		store, err := service.NewStoreIdentity(p.identities)
		if err != nil {
			t.Fatalf("artistpage.NewStoreIdentity: %v", err)
		}
		identity = store
	}
	var anchoring service.Anchoring = service.NoAnchoring{}
	if p.anchor != nil {
		musicBrainz, err := service.NewMusicBrainzAnchoring(p.anchor, p.breaker)
		if err != nil {
			t.Fatalf("artistpage.NewMusicBrainzAnchoring: %v", err)
		}
		anchoring = musicBrainz
	}
	var observer service.DiscographyObserver = service.NoDiscographyObserver{}
	if p.events != nil {
		telemetry, err := service.NewDiscographyTelemetry(p.events, p.tracker, clock.Now)
		if err != nil {
			t.Fatalf("artistpage.NewDiscographyTelemetry: %v", err)
		}
		observer = telemetry
	}
	svc, err := service.NewGetArtistContentService(providers, p.tracklists, identity, anchoring, observer, p.breaker)
	if err != nil {
		t.Fatalf("artistpage.NewGetArtistContentService: %v", err)
	}
	return svc
}

type albumTracksParts struct {
	featured service.DeezerFeaturedLookup
	fallback ports.SearchProvider
	breaker  *service.CircuitBreaker
}

type AlbumTracksPart func(*albumTracksParts)

func AlbumFeatured(lookup service.DeezerFeaturedLookup) AlbumTracksPart {
	return func(p *albumTracksParts) { p.featured = lookup }
}

func AlbumFallbackSearcher(searcher ports.SearchProvider) AlbumTracksPart {
	return func(p *albumTracksParts) { p.fallback = searcher }
}

func AlbumBreaker(breaker *service.CircuitBreaker) AlbumTracksPart {
	return func(p *albumTracksParts) { p.breaker = breaker }
}

func NewAlbumTracks(t testing.TB, providers map[domain.ProviderName]ports.AlbumContentProvider, parts ...AlbumTracksPart) *service.GetAlbumTracksService {
	t.Helper()
	p := albumTracksParts{breaker: service.NewCircuitBreaker(clock.Now)}
	for _, part := range parts {
		part(&p)
	}
	var featured service.FeaturedEnrichment = service.NoFeaturedEnrichment{}
	if p.featured != nil {
		deezer, err := service.NewDeezerFeaturedEnrichment(p.featured, p.breaker)
		if err != nil {
			t.Fatalf("artistpage.NewDeezerFeaturedEnrichment: %v", err)
		}
		featured = deezer
	}
	var fallback service.AlbumFallback = service.NoAlbumFallback{}
	if p.fallback != nil {
		search, err := service.NewSearchAlbumFallback(p.fallback, p.breaker)
		if err != nil {
			t.Fatalf("artistpage.NewSearchAlbumFallback: %v", err)
		}
		fallback = search
	}
	svc, err := service.NewGetAlbumTracksService(providers, featured, fallback, p.breaker)
	if err != nil {
		t.Fatalf("artistpage.NewGetAlbumTracksService: %v", err)
	}
	return svc
}

func NewFeaturedResolver(t testing.TB, mb service.MBFeaturedSearcher, deezer service.DeezerFeaturedLookup) *service.FeaturedArtistResolver {
	t.Helper()
	resolver, err := service.NewFeaturedArtistResolver(mb, deezer)
	if err != nil {
		t.Fatalf("artistpage.NewFeaturedArtistResolver: %v", err)
	}
	return resolver
}
