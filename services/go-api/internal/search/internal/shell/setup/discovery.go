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
	wiring            wiring
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
	services := newHandlerServices(w)
	unreleasedServices := newUnreleasedServices(d, t, s, services.Artist)
	return &Discovery{
		Handler:           newHandler(d, t, s, services, unreleasedServices.lookup),
		Search:            services.Search,
		FeaturedArtists:   newFeaturedArtists(t),
		Recordings:        t.named(rowMusicBrainz).recordings,
		Charts:            t.chartProviders(),
		SpellingHints:     s.SpellingHints,
		UnreleasedRefresh: unreleasedServices.refresh,
		Events:            s.Events,
		wiring:            w,
	}, nil
}

func newWiring(d connections.Deps) wiring {
	s := connections.NewStores(d)
	t := newProviderTable(d.Providers, connections.NewClients(d), s)
	t.log(d.Lifecycle, d.Logger)
	return wiring{deps: d, table: t, stores: s, artwork: t.artworkChain()}
}

func (d *Discovery) RankingOnlySearch() *query.Service {
	return newRankingOnlySearchService(d.wiring)
}

func newHandlerServices(w wiring) handler.DiscoveryServices {
	breaker := providerguard.NewCircuitBreaker()
	t, s := w.table, w.stores
	return handler.DiscoveryServices{
		Search:        newSearchService(w, breaker),
		History:       history.NewListSearchHistoryService(s.History),
		RecordHistory: history.NewRecordSearchHistoryService(s.History),
		ClearHistory:  history.NewClearSearchHistoryService(s.History),
		Album:         newAlbumService(t, breaker),
		Artist:        newArtistService(t, s, breaker, w.deps.Tracker),
		Related:       newRelatedService(t, breaker),
		Enrich:        newEnrichmentService(t, s, w.artwork),
		Suggest:       vocabulary.NewSuggestService(s.SpellingHints),
		Event:         newEventService(w.deps.Activity, s),
		Favorites:     favorites.NewFavoritesService(persistence.NewPostgresFavoritesRepository(s.Pool)),
	}
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
