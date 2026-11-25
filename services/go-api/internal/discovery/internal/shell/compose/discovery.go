package compose

import (
	"altune/go-api/internal/discovery/adapters/handler"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/adapters/persistence"
	"altune/go-api/internal/discovery/internal/core/favorites"
	"altune/go-api/internal/discovery/internal/core/history"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/content"
	"altune/go-api/internal/discovery/service/interaction"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/discovery/service/vocabulary"
	"context"
	"time"
)

type RecordingAuthority interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]ports.ISRCRecording, error)
	RecordingByMBID(ctx context.Context, mbid string) (ports.MBRecording, bool, error)
	WorkRecordings(ctx context.Context, recordingMBID string) ([]ports.MBWorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (ports.MBRecordingDetail, bool, error)
}

type EventRetention interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
	PruneEvents(ctx context.Context, now time.Time) (int64, error)
}

type Discovery struct {
	Handler           *handler.DiscoveryHandler
	Search            *search.Service
	FeaturedArtists   *content.FeaturedArtistResolver
	Recordings        RecordingAuthority
	Charts            []ports.ChartProvider
	Vocabulary        ports.VocabularyStore
	UnreleasedRefresh *unreleased.Service
	Events            EventRetention
	wiring            wiring
}

type wiring struct {
	deps    handles.Deps
	table   *providerTable
	stores  handles.Stores
	artwork ports.TaggingArtworkResolver
}

func New(d handles.Deps) (*Discovery, error) {
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
		Vocabulary:        s.Vocabulary,
		UnreleasedRefresh: unreleasedServices.refresh,
		Events:            s.Events,
		wiring:            w,
	}, nil
}

func newWiring(d handles.Deps) wiring {
	s := handles.NewStores(d)
	t := newProviderTable(d.Providers, handles.NewClients(d), s)
	t.log(d.Lifecycle, d.Logger)
	return wiring{deps: d, table: t, stores: s, artwork: t.artworkChain()}
}

func (d *Discovery) RankingOnlySearch() *search.Service {
	return newRankingOnlySearchService(d.wiring)
}

func newHandlerServices(w wiring) handler.DiscoveryServices {
	breaker := providercall.NewCircuitBreaker()
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
		Suggest:       vocabulary.NewSuggestService(s.Vocabulary),
		Event:         newEventService(w.deps.Activity, s),
		Favorites:     favorites.NewFavoritesService(persistence.NewPgxFavoritesRepository(s.Pool)),
	}
}

func newEventService(activity ports.ActivityFeed, s handles.Stores) *interaction.RecordEventService {
	return interaction.NewRecordEventService(s.Events, interaction.WithRecordEventActivityFeed(activity))
}

func newHandler(d handles.Deps, t *providerTable, s handles.Stores, services handler.DiscoveryServices, unreleasedLookup *unreleased.Service) *handler.DiscoveryHandler {
	h := handler.NewDiscoveryHandler(services)
	h.WithDetailEnrichers(newDetailEnrichers(t, s))
	h.WithUnreleased(unreleasedLookup)
	return h.WithClientEvents(d.ClientEvents)
}
