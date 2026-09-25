package handler

import (
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/shared/httputil"
	"time"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc       *service.Service
	historySvc      *service.ListSearchHistoryService
	clearHistorySvc *service.ClearSearchHistoryService
	albumSvc        *service.GetAlbumTracksService
	artistSvc       *service.GetArtistContentService
	relatedSvc      *service.GetRelatedTracksService
	enrichSvc       *enrich.EnrichmentService
	suggestSvc      *service.SuggestService
	eventSvc        *service.RecordEventService
	favoritesSvc    *service.FavoritesService

	enrichers DetailEnrichers

	ownership *service.OwnershipEnrichmentService

	searchLimiter    *userRateLimiter
	suggestLimiter   *userRateLimiter
	eventLimiter     *userRateLimiter
	contentLimiter   *userRateLimiter
	favoritesLimiter *userRateLimiter
}

type DetailEnrichers struct {
	LastFm *enrich.LastFmEnrichmentService
	Deezer *enrich.DeezerEnrichmentService
	Lyrics *enrich.LyricsService
}

func (h *DiscoveryHandler) WithDetailEnrichers(e DetailEnrichers) *DiscoveryHandler {
	h.enrichers = e
	return h
}

type DiscoveryServices struct {
	Search       *service.Service
	History      *service.ListSearchHistoryService
	ClearHistory *service.ClearSearchHistoryService
	Album        *service.GetAlbumTracksService
	Artist       *service.GetArtistContentService
	Related      *service.GetRelatedTracksService
	Enrich       *enrich.EnrichmentService
	Suggest      *service.SuggestService
	Event        *service.RecordEventService
	Favorites    *service.FavoritesService
}

// WithRateLimits replaces DefaultDiscoveryRateLimits on the throttled routes.
// Call it before Routes.
func (h *DiscoveryHandler) WithRateLimits(limits DiscoveryRateLimits) *DiscoveryHandler {
	h.searchLimiter = newUserRateLimiter(limits.Search, time.Now)
	h.suggestLimiter = newUserRateLimiter(limits.Suggest, time.Now)
	h.eventLimiter = newUserRateLimiter(limits.Events, time.Now)
	h.contentLimiter = newUserRateLimiter(limits.Content, time.Now)
	h.favoritesLimiter = newUserRateLimiter(limits.Favorites, time.Now)
	return h
}

func NewDiscoveryHandler(svcs DiscoveryServices) *DiscoveryHandler {
	h := &DiscoveryHandler{
		searchSvc:       svcs.Search,
		historySvc:      svcs.History,
		clearHistorySvc: svcs.ClearHistory,
		albumSvc:        svcs.Album,
		artistSvc:       svcs.Artist,
		relatedSvc:      svcs.Related,
		enrichSvc:       svcs.Enrich,
		suggestSvc:      svcs.Suggest,
		eventSvc:        svcs.Event,
		favoritesSvc:    svcs.Favorites,
	}
	return h.WithRateLimits(DefaultDiscoveryRateLimits)
}

// maxEventBodyBytes keeps an event body from being decoded at the global 1 MiB
// limit only to be rejected by the service's payload cap. It sits well above
// that cap, so an oversized payload still answers with the typed payload error
// rather than an unparseable-body one.
const maxEventBodyBytes = 32 << 10

const maxFavoriteBodyBytes = 16 << 10

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(h.searchLimiter.middleware).Get("/search", h.handleSearch)
	r.With(h.suggestLimiter.middleware).Get("/suggest", h.handleSuggest)
	r.Get("/search-history", h.handleSearchHistory)
	r.Delete("/search-history", h.handleClearSearchHistory)
	r.With(h.eventLimiter.middleware, httputil.MaxBodySize(maxEventBodyBytes)).Post("/events", h.handleRecordEvent)
	r.Get("/favorites", h.handleListFavorites)
	r.With(h.favoritesLimiter.middleware, httputil.MaxBodySize(maxFavoriteBodyBytes)).Put("/favorites", h.handleAddFavorite)
	r.With(h.favoritesLimiter.middleware, httputil.MaxBodySize(maxFavoriteBodyBytes)).Delete("/favorites", h.handleRemoveFavorite)
	r.Group(h.contentRoutes)
	return r
}

// contentRoutes are the provider fan-out routes, which share one per-user
// budget because they spend one shared provider quota.
func (h *DiscoveryHandler) contentRoutes(r chi.Router) {
	r.Use(h.contentLimiter.middleware)
	r.Get("/albums/{provider}/{externalId}/tracks", h.handleAlbumTracks)
	r.Get("/artists/{provider}/{externalId}/content", h.handleArtistContent)
	r.Get("/artists/{provider}/{externalId}/top-tracks", h.handleArtistTopTracks)
	r.Get("/artists/{provider}/{externalId}/albums", h.handleArtistAlbums)
	r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)
	r.Get("/enrichment", h.handleEnrichment)
	r.Get("/enrichment/lastfm", h.handleLastFmEnrichment)
	r.Get("/enrichment/deezer", h.handleDeezerEnrichment)
	r.Get("/lyrics", h.handleLyrics)
}
