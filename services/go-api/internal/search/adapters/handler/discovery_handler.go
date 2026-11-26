package handler

import (
	"altune/go-api/internal/search/internal/core/details"
	"altune/go-api/internal/search/internal/core/favorites"
	"altune/go-api/internal/search/internal/core/history"
	"altune/go-api/internal/search/service/artistpage"
	"altune/go-api/internal/search/service/inlibrary"
	"altune/go-api/internal/search/service/query"
	"altune/go-api/internal/search/service/related"
	"altune/go-api/internal/search/service/unreleased"
	"altune/go-api/internal/search/service/useractivity"
	"altune/go-api/internal/search/service/vocabulary"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/telemetry"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc       *query.Service
	historySvc      *history.ListSearchHistoryService
	recordHistory   *history.RecordSearchHistoryService
	clearHistorySvc *history.ClearSearchHistoryService
	albumSvc        *artistpage.GetAlbumTracksService
	artistSvc       *artistpage.GetArtistContentService
	relatedSvc      *related.GetRelatedTracksService
	unreleasedSvc   *unreleased.Service
	enrichSvc       *details.DetailsService
	suggestSvc      *vocabulary.SuggestService
	eventSvc        *useractivity.RecordEventService
	favoritesSvc    *favorites.FavoritesService
	clientEvents    *telemetry.ClientEvents

	enrichers DetailEnrichers

	ownership *inlibrary.OwnershipEnrichmentService

	searchLimiter    func(http.Handler) http.Handler
	suggestLimiter   func(http.Handler) http.Handler
	eventLimiter     func(http.Handler) http.Handler
	contentLimiter   func(http.Handler) http.Handler
	favoritesLimiter func(http.Handler) http.Handler
	historyLimiter   func(http.Handler) http.Handler
}

type DetailEnrichers struct {
	Deezer *details.DeezerEnrichmentService
	Lyrics *details.LyricsService
}

func (h *DiscoveryHandler) WithDetailEnrichers(e DetailEnrichers) *DiscoveryHandler {
	h.enrichers = e
	return h
}

type DiscoveryServices struct {
	Search        *query.Service
	History       *history.ListSearchHistoryService
	RecordHistory *history.RecordSearchHistoryService
	ClearHistory  *history.ClearSearchHistoryService
	Album         *artistpage.GetAlbumTracksService
	Artist        *artistpage.GetArtistContentService
	Related       *related.GetRelatedTracksService
	Enrich        *details.DetailsService
	Suggest       *vocabulary.SuggestService
	Event         *useractivity.RecordEventService
	Favorites     *favorites.FavoritesService
}

func (h *DiscoveryHandler) WithRateLimits(limits DiscoveryRateLimits) *DiscoveryHandler {
	return h.WithRateLimitsAt(limits, time.Now)
}

func (h *DiscoveryHandler) WithRateLimitsAt(limits DiscoveryRateLimits, now func() time.Time) *DiscoveryHandler {
	h.searchLimiter = newUserRateLimiter(limits.Search, now)
	h.suggestLimiter = newUserRateLimiter(limits.Suggest, now)
	h.eventLimiter = newUserRateLimiter(limits.Events, now)
	h.contentLimiter = newUserRateLimiter(limits.Content, now)
	h.favoritesLimiter = newUserRateLimiter(limits.Favorites, now)
	h.historyLimiter = newUserRateLimiter(limits.History, now)
	return h
}

func (h *DiscoveryHandler) WithClientEvents(c *telemetry.ClientEvents) *DiscoveryHandler {
	h.clientEvents = c
	return h
}

func NewDiscoveryHandler(svcs DiscoveryServices) *DiscoveryHandler {
	h := &DiscoveryHandler{
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
	}
	return h.WithRateLimits(DefaultDiscoveryRateLimits)
}

const maxEventBodyBytes = 32 << 10

const maxFavoriteBodyBytes = 16 << 10

const maxHistoryBodyBytes = 4 << 10

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(noStorePerUser, h.searchLimiter).Get("/search", h.handleSearch)
	r.With(h.suggestLimiter).Get("/suggest", h.handleSuggest)
	r.With(noStorePerUser, h.historyLimiter).Get("/search-history", h.handleSearchHistory)
	r.With(h.historyLimiter, httpserver.MaxBodySize(maxHistoryBodyBytes)).Post("/search-history", h.handleRecordSearchHistory)
	r.With(h.historyLimiter).Delete("/search-history", h.handleClearSearchHistory)
	r.With(h.eventLimiter, httpserver.MaxBodySize(maxEventBodyBytes)).Post("/events", h.handleRecordEvent)
	r.With(noStorePerUser, h.favoritesLimiter).Get("/favorites", h.handleListFavorites)
	r.With(h.favoritesLimiter, httpserver.MaxBodySize(maxFavoriteBodyBytes)).Put("/favorites", h.handleAddFavorite)
	r.With(h.favoritesLimiter, httpserver.MaxBodySize(maxFavoriteBodyBytes)).Delete("/favorites", h.handleRemoveFavorite)
	r.Group(h.contentRoutes)
	return r
}

func (h *DiscoveryHandler) contentRoutes(r chi.Router) {
	r.Use(requireUser, h.contentLimiter)
	r.Get("/albums/{provider}/{externalId}/tracks", h.handleAlbumTracks)
	r.Get("/artists/{provider}/{externalId}/content", h.handleArtistContent)
	r.Get("/artists/{provider}/{externalId}/top-tracks", h.handleArtistTopTracks)
	r.Get("/artists/{provider}/{externalId}/albums", h.handleArtistAlbums)
	r.Get("/artists/{provider}/{externalId}/unreleased", h.handleArtistUnreleased)
	r.Get("/unreleased/search", h.handleUnreleasedSearch)
	r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)
	r.Get("/enrichment", h.handleEnrichment)
	r.Get("/enrichment/deezer", h.handleDeezerEnrichment)
	r.Get("/lyrics", h.handleLyrics)
}

func noStorePerUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requestuser.RequireUserID(w, r); !ok {
			return
		}
		next.ServeHTTP(w, r)
	})
}
