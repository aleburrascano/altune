package handler

import (
	"altune/go-api/internal/discovery/adapters/authbridge"
	"altune/go-api/internal/discovery/service/content"
	"altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/discovery/service/favorites"
	"altune/go-api/internal/discovery/service/history"
	"altune/go-api/internal/discovery/service/interaction"
	"altune/go-api/internal/discovery/service/ownership"
	"altune/go-api/internal/discovery/service/related"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/discovery/service/vocabulary"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/telemetry"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc       *search.Service
	historySvc      *history.ListSearchHistoryService
	recordHistory   *history.RecordSearchHistoryService
	clearHistorySvc *history.ClearSearchHistoryService
	albumSvc        *content.GetAlbumTracksService
	artistSvc       *content.GetArtistContentService
	relatedSvc      *related.GetRelatedTracksService
	unreleasedSvc   *unreleased.Service
	enrichSvc       *enrich.EnrichmentService
	suggestSvc      *vocabulary.SuggestService
	eventSvc        *interaction.RecordEventService
	favoritesSvc    *favorites.FavoritesService
	clientEvents    *telemetry.ClientEvents

	enrichers DetailEnrichers

	ownership *ownership.OwnershipEnrichmentService

	searchLimiter    func(http.Handler) http.Handler
	suggestLimiter   func(http.Handler) http.Handler
	eventLimiter     func(http.Handler) http.Handler
	contentLimiter   func(http.Handler) http.Handler
	favoritesLimiter func(http.Handler) http.Handler
	historyLimiter   func(http.Handler) http.Handler
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
	Search        *search.Service
	History       *history.ListSearchHistoryService
	RecordHistory *history.RecordSearchHistoryService
	ClearHistory  *history.ClearSearchHistoryService
	Album         *content.GetAlbumTracksService
	Artist        *content.GetArtistContentService
	Related       *related.GetRelatedTracksService
	Enrich        *enrich.EnrichmentService
	Suggest       *vocabulary.SuggestService
	Event         *interaction.RecordEventService
	Favorites     *favorites.FavoritesService
}

func (h *DiscoveryHandler) WithRateLimits(limits DiscoveryRateLimits) *DiscoveryHandler {
	h.searchLimiter = newUserRateLimiter(limits.Search, time.Now)
	h.suggestLimiter = newUserRateLimiter(limits.Suggest, time.Now)
	h.eventLimiter = newUserRateLimiter(limits.Events, time.Now)
	h.contentLimiter = newUserRateLimiter(limits.Content, time.Now)
	h.favoritesLimiter = newUserRateLimiter(limits.Favorites, time.Now)
	h.historyLimiter = newUserRateLimiter(limits.History, time.Now)
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
	r.With(h.historyLimiter, httputil.MaxBodySize(maxHistoryBodyBytes)).Post("/search-history", h.handleRecordSearchHistory)
	r.With(h.historyLimiter).Delete("/search-history", h.handleClearSearchHistory)
	r.With(h.eventLimiter, httputil.MaxBodySize(maxEventBodyBytes)).Post("/events", h.handleRecordEvent)
	r.With(noStorePerUser, h.favoritesLimiter).Get("/favorites", h.handleListFavorites)
	r.With(h.favoritesLimiter, httputil.MaxBodySize(maxFavoriteBodyBytes)).Put("/favorites", h.handleAddFavorite)
	r.With(h.favoritesLimiter, httputil.MaxBodySize(maxFavoriteBodyBytes)).Delete("/favorites", h.handleRemoveFavorite)
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
	r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)
	r.Get("/enrichment", h.handleEnrichment)
	r.Get("/enrichment/lastfm", h.handleLastFmEnrichment)
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
		if _, ok := authbridge.RequireUserID(w, r); !ok {
			return
		}
		next.ServeHTTP(w, r)
	})
}
