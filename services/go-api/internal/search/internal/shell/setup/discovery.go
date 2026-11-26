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
	w := newWiring(d)
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
	return &Discovery{
		Handler:           newHandler(d, t, s, services, unreleasedServices.lookup),
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

func newWiring(d connections.Deps) wiring {
	s := connections.NewStores(d)
	t := newProviderTable(d.Providers, connections.NewClients(d), s)
	t.log(d.Lifecycle, d.Logger)
	return wiring{deps: d, table: t, stores: s, artwork: t.artworkChain()}
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
	return handler.DiscoveryServices{
		Search:        search,
		History:       history.NewListSearchHistoryService(s.History),
		RecordHistory: recordHistory,
		ClearHistory:  history.NewClearSearchHistoryService(s.History, time.Now),
		Album:         album,
		Artist:        artist,
		Related:       relatedTracks,
		Enrich:        enrichment,
		Suggest:       vocabulary.NewSuggestService(s.SpellingHints),
		Event:         newEventService(w.deps.Activity, s),
		Favorites:     favorites.NewFavoritesService(persistence.NewPostgresFavoritesRepository(s.Pool)),
	}, nil
}

func newEventService(activity ports.ActivityFeed, s connections.Stores) *useractivity.RecordEventService {
	return useractivity.NewRecordEventService(s.Events, useractivity.WithRecordEventActivityFeed(activity))
}

func newHandler(d connections.Deps, t *providerTable, s connections.Stores, services handler.DiscoveryServices, unreleasedLookup *unreleased.Service) *handler.DiscoveryHandler {
	h := handler.NewDiscoveryHandler(services)
	h.WithDetailEnrichers(newDetailEnrichers(t, s))
	h.WithUnreleased(unreleasedLookup)
	return h.WithClientEvents(d.ClientEvents)
}
