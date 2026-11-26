package setup

import (
	"altune/go-api/internal/search/adapters/handler"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/favorites"
	"altune/go-api/internal/search/internal/core/history"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/artistpage"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/search/service/query"
	"altune/go-api/internal/search/service/unreleased"
	"altune/go-api/internal/search/service/useractivity"
	"altune/go-api/internal/search/service/vocabulary"
	"context"
	"errors"
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

func New(d connections.Deps) (*Discovery, error) {
	if err := d.MissingHandles(); err != nil {
		return nil, err
	}
	w, err := newWiring(d)
	if err != nil {
		return nil, err
	}
	t, s := w.table, w.stores
	services, err := newHandlerServices(w)
	if err != nil {
		return nil, err
	}
	unreleasedServices, err := newUnreleasedServices(d, t, s, services.Artist)
	if err != nil {
		return nil, err
	}
	rankingOnly, err := newRankingOnlySearchService(w)
	if err != nil {
		return nil, err
	}
	featuredArtists, err := newFeaturedArtists(t)
	if err != nil {
		return nil, fmt.Errorf("build the featured artist resolver: %w", err)
	}
	discoveryHandler, err := newHandler(d, t, s, services, unreleasedServices.lookup)
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

func newWiring(d connections.Deps) (wiring, error) {
	s, err := connections.NewStores(d)
	if err != nil {
		return wiring{}, fmt.Errorf("build the stores: %w", err)
	}
	t, err := newProviderTable(d.Providers, connections.NewClients(d), s)
	if err != nil {
		return wiring{}, fmt.Errorf("build the provider table: %w", err)
	}
	t.log(d.Lifecycle, d.Logger)
	return wiring{deps: d, table: t, stores: s, artwork: t.artworkChain()}, nil
}

func (d *Discovery) RankingOnlySearch() *query.Service {
	return d.rankingOnly
}

func newHandlerServices(w wiring) (handler.DiscoveryServices, error) {
	breaker := providerguard.NewCircuitBreaker()
	t, s := w.table, w.stores
	enrichment, err := newEnrichmentService(t, s, w.artwork)
	if err != nil && !errors.Is(err, errEnrichmentSwitchedOff) {
		return handler.DiscoveryServices{}, fmt.Errorf("build the enrichment service: %w", err)
	}
	search, err := newSearchService(w, breaker)
	if err != nil {
		return handler.DiscoveryServices{}, err
	}
	recordHistory, err := history.NewRecordSearchHistoryService(s.History, time.Now)
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
	clearHistory, err := history.NewClearSearchHistoryService(s.History, time.Now)
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
		Suggest:       vocabulary.NewSuggestService(s.SpellingHints),
		Event:         eventService,
		Favorites:     favoritesService,
	}, nil
}

func newEventService(activity ports.ActivityFeed, s connections.Stores) (*useractivity.RecordEventService, error) {
	if activity == nil {
		activity = useractivity.NoopActivityFeed{}
	}
	return useractivity.NewRecordEventService(s.Events, activity)
}

func newHandler(d connections.Deps, t *providerTable, s connections.Stores, services handler.DiscoveryServices, unreleasedLookup *unreleased.Service) (*handler.DiscoveryHandler, error) {
	enrichers, err := newDetailEnrichers(t, s)
	if err != nil {
		return nil, err
	}
	h, err := handler.NewDiscoveryHandler(services, time.Now)
	if err != nil {
		return nil, fmt.Errorf("build the discovery handler: %w", err)
	}
	h.WithDetailEnrichers(enrichers)
	h.WithUnreleased(unreleasedLookup)
	return h.WithClientEvents(d.ClientEvents), nil
}
