package setup

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/internal/service/favorites"
	"altune/go-api/internal/search/internal/service/history"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/search/internal/service/query"
	"altune/go-api/internal/search/internal/service/unreleased"
	"altune/go-api/internal/search/internal/service/useractivity"
	"altune/go-api/internal/search/internal/service/vocabulary"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
	"time"
)

type RecordingAuthority interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]ports.ISRCRecording, error)
	RecordingByMBID(ctx context.Context, mbid string) (ports.MusicBrainzRecording, bool, error)
	WorkRecordings(ctx context.Context, recordingMBID string) ([]ports.MusicBrainzWorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (ports.MusicBrainzRecordingDetail, bool, error)
}

type EventRetention interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

type Discovery struct {
	Handler           *handler.DiscoveryHandler
	Search            *query.Service
	FeaturedArtists   *artistpage.FeaturedArtistResolver
	Recordings        RecordingAuthority
	Charts            []ports.ChartProvider
	SpellingHints     ports.SpellingHintsStore
	UnreleasedRefresh *unreleased.Service
	Events            EventRetention
	rankingOnly       *query.Service
}

type wiring struct {
	deps    connections.Deps
	table   *providerTable
	stores  connections.Stores
	artwork ports.TaggingArtworkLookup
}

func New(ctx context.Context, d connections.Deps) (*Discovery, error) {
	if err := d.MissingHandles(); err != nil {
		return nil, fmt.Errorf("set up discovery: %w", err)
	}
	w, err := newWiring(ctx, d)
	if err != nil {
		return nil, err
	}
	t, s := w.table, w.stores
	services, err := newHandlerServices(ctx, w)
	if err != nil {
		return nil, err
	}
	unreleasedServices, err := newUnreleasedServices(ctx, d, t, s, services.Artist)
	if err != nil {
		return nil, err
	}
	rankingOnly, err := newRankingOnlySearchService(ctx, w)
	if err != nil {
		return nil, err
	}
	featuredArtists, err := newFeaturedArtists(t)
	if err != nil {
		return nil, fmt.Errorf("build the featured artist resolver: %w", err)
	}
	discoveryHandler, err := newHandler(services, unreleasedServices.lookup)
	if err != nil {
		return nil, err
	}
	return &Discovery{
		Handler:           discoveryHandler,
		Search:            services.Search,
		FeaturedArtists:   featuredArtists,
		Recordings:        t.named(rowMusicBrainz).recordings,
		Charts:            t.chartProviders(),
		SpellingHints:     s.SpellingHints,
		UnreleasedRefresh: unreleasedServices.refresh,
		Events:            s.Events,
		rankingOnly:       rankingOnly,
	}, nil
}

func newWiring(ctx context.Context, d connections.Deps) (wiring, error) {
	s, err := connections.NewStores(d)
	if err != nil {
		return wiring{}, fmt.Errorf("build the stores: %w", err)
	}
	t, err := newProviderTable(d.Providers, connections.NewClients(d), s)
	if err != nil {
		return wiring{}, fmt.Errorf("build the provider table: %w", err)
	}
	t.log(ctx, d.Logger)
	return wiring{deps: d, table: t, stores: s, artwork: t.artworkChain()}, nil
}

func (d *Discovery) RankingOnlySearch() *query.Service {
	return d.rankingOnly
}

func newHandlerServices(ctx context.Context, w wiring) (handler.DiscoveryServices, error) {
	breaker := providerguard.NewCircuitBreaker(clock.Now)
	t, s := w.table, w.stores
	enrichment, err := newEnrichment(t, s, w.artwork)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the enrichment service: %w", err)
	}
	enrichers, err := newDetailEnrichers(t, s)
	if err != nil {
		return handler.DiscoveryServices{}, err
	}
	search, err := newSearchService(ctx, w, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, err
	}
	recordHistory, err := history.NewRecordSearchHistoryService(s.History, clock.Now, ids.Real)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the history recorder: %w", err)
	}
	album, err := newAlbumService(t, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the album tracks service: %w", err)
	}
	artist, err := newArtistService(t, s, breaker, w.deps.Tracker)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the artist content service: %w", err)
	}
	relatedTracks, err := newRelatedService(t, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the related tracks service: %w", err)
	}
	listHistory, err := history.NewListSearchHistoryService(s.History)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the history list service: %w", err)
	}
	clearHistory, err := history.NewClearSearchHistoryService(s.History, clock.Now)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the clear-history service: %w", err)
	}
	favoritesService, err := favorites.NewFavoritesService(persistence.NewPostgresFavoritesRepository(s.Pool))
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the favorites service: %w", err)
	}
	eventService, err := newEventService(w.deps.Activity, s)
	if err != nil {
		return handler.DiscoveryServices{}, fmt.Errorf("build the event service: %w", err)
	}
	return handler.DiscoveryServices{
		Search:        search,
		History:       listHistory,
		RecordHistory: recordHistory,
		ClearHistory:  clearHistory,
		Album:         album,
		Artist:        artist,
		Related:       relatedTracks,
		Enrich:        enrichment,
		Enrichers:     enrichers,
		Ownership:     handler.NoOwnership{},
		ClientEvents:  w.deps.ClientEvents,
		Suggest:       vocabulary.NewSuggestService(s.SpellingHints),
		Event:         eventService,
		Favorites:     favoritesService,
	}, nil
}

func newEventService(activity ports.ActivityFeed, s connections.Stores) (*useractivity.RecordEventService, error) {
	r0, err := useractivity.NewRecordEventService(s.Events, activity, clock.Now)
	if err != nil {
		return r0, fmt.Errorf("new record event service: %w", err)
	}
	return r0, nil
}

func newHandler(services handler.DiscoveryServices, unreleasedLookup handler.UnreleasedReader) (*handler.DiscoveryHandler, error) {
	services.Unreleased = unreleasedLookup
	h, err := handler.NewDiscoveryHandler(services, clock.Now)
	if err != nil {
		return nil, fmt.Errorf("build the discovery handler: %w", err)
	}
	return h, nil
}
