package handler

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"fmt"
	"net/http"
	"strings"
)

type DiscoveryHandler struct {
	now             clock.Func
	searchSvc       *service.SearchService
	historySvc      *service.ListSearchHistoryService
	recordHistory   *service.RecordSearchHistoryService
	clearHistorySvc *service.ClearSearchHistoryService
	albumSvc        *service.GetAlbumTracksService
	artistSvc       *service.GetArtistContentService
	relatedSvc      *service.GetRelatedTracksService
	unreleasedSvc   UnreleasedReader
	enrichSvc       Enricher
	suggestSvc      *service.SuggestService
	eventSvc        *service.RecordEventService
	favoritesSvc    *service.FavoritesService
	clientEvents    ClientEventSink
	enrichers       DetailEnrichers
	ownership       OwnershipStamper

	searchLimiter    func(http.Handler) http.Handler
	suggestLimiter   func(http.Handler) http.Handler
	eventLimiter     func(http.Handler) http.Handler
	contentLimiter   func(http.Handler) http.Handler
	favoritesLimiter func(http.Handler) http.Handler
	historyLimiter   func(http.Handler) http.Handler
}

type DetailEnrichers struct {
	Deezer *service.DeezerEnrichmentService
	Lyrics *service.LyricsService
}

type UnreleasedReader interface {
	Search(ctx context.Context, query string) ([]ports.UnreleasedHit, error)
	Lookup(ctx context.Context, provider domain.ProviderName, externalID, artistName string, known map[domain.ProviderName]string) (service.Lookup, error)
	Warm(ctx context.Context, artists []service.ArtistToRefresh)
}

type NoUnreleased struct{}

func (NoUnreleased) Search(context.Context, string) ([]ports.UnreleasedHit, error) {
	return []ports.UnreleasedHit{}, nil
}

func (NoUnreleased) Lookup(context.Context, domain.ProviderName, string, string, map[domain.ProviderName]string) (service.Lookup, error) {
	return service.Lookup{}, nil
}

func (NoUnreleased) Warm(context.Context, []service.ArtistToRefresh) {}

type ClientEventSink interface {
	Emit(ctx context.Context, event telemetry.ClientEvent)
}

type NoClientEvents struct{}

func (NoClientEvents) Emit(context.Context, telemetry.ClientEvent) {}

type DiscoveryServices struct {
	Search        *service.SearchService
	History       *service.ListSearchHistoryService
	RecordHistory *service.RecordSearchHistoryService
	ClearHistory  *service.ClearSearchHistoryService
	Album         *service.GetAlbumTracksService
	Artist        *service.GetArtistContentService
	Related       *service.GetRelatedTracksService
	Enrich        Enricher
	Suggest       *service.SuggestService
	Event         *service.RecordEventService
	Favorites     *service.FavoritesService
	Unreleased    UnreleasedReader
	Enrichers     DetailEnrichers
	Ownership     OwnershipStamper
	ClientEvents  ClientEventSink
}

func (s DiscoveryServices) missing() []string {
	checks := []struct {
		name    string
		missing bool
	}{
		{"search", s.Search == nil},
		{"history", s.History == nil},
		{"record history", s.RecordHistory == nil},
		{"clear history", s.ClearHistory == nil},
		{"album", s.Album == nil},
		{"artist", s.Artist == nil},
		{"related", s.Related == nil},
		{"enrich", s.Enrich == nil},
		{"suggest", s.Suggest == nil},
		{"event", s.Event == nil},
		{"favorites", s.Favorites == nil},
		{"unreleased", s.Unreleased == nil},
		{"deezer enricher", s.Enrichers.Deezer == nil},
		{"lyrics", s.Enrichers.Lyrics == nil},
		{"ownership", s.Ownership == nil},
		{"client events", s.ClientEvents == nil},
	}
	var names []string
	for _, c := range checks {
		if c.missing {
			names = append(names, c.name)
		}
	}
	return names
}

func (h *DiscoveryHandler) WithRateLimits(limits DiscoveryRateLimits) *DiscoveryHandler {
	h.searchLimiter = newUserRateLimiter(limits.Search, h.now)
	h.suggestLimiter = newUserRateLimiter(limits.Suggest, h.now)
	h.eventLimiter = newUserRateLimiter(limits.Events, h.now)
	h.contentLimiter = newUserRateLimiter(limits.Content, h.now)
	h.favoritesLimiter = newUserRateLimiter(limits.Favorites, h.now)
	h.historyLimiter = newUserRateLimiter(limits.History, h.now)
	return h
}

func MustNewDiscoveryHandler(svcs DiscoveryServices, now clock.Func) *DiscoveryHandler {
	h, err := NewDiscoveryHandler(svcs, now)
	if err != nil {
		panic(err)
	}
	return h
}

func NewDiscoveryHandler(svcs DiscoveryServices, now clock.Func) (*DiscoveryHandler, error) {
	if now == nil {
		return nil, domain.ErrHandlerClockRequired
	}
	if names := svcs.missing(); len(names) > 0 {
		return nil, fmt.Errorf("%w: missing %s", domain.ErrHandlerServiceRequired, strings.Join(names, ", "))
	}
	h := &DiscoveryHandler{
		now:             now,
		searchSvc:       svcs.Search,
		historySvc:      svcs.History,
		recordHistory:   svcs.RecordHistory,
		clearHistorySvc: svcs.ClearHistory,
		albumSvc:        svcs.Album,
		artistSvc:       svcs.Artist,
		relatedSvc:      svcs.Related,
		enrichSvc:       svcs.Enrich,
		suggestSvc:      svcs.Suggest,
		eventSvc:        svcs.Event,
		favoritesSvc:    svcs.Favorites,
		unreleasedSvc:   svcs.Unreleased,
		enrichers:       svcs.Enrichers,
		ownership:       svcs.Ownership,
		clientEvents:    svcs.ClientEvents,
	}
	return h.WithRateLimits(DefaultDiscoveryRateLimits), nil
}
