package handler

import (
	"altune/go-api/internal/shared/httpserver"

	"github.com/go-chi/chi/v5"
)

type Routes struct {
	discovery *DiscoveryHandler
}

func NewRoutes(discovery *DiscoveryHandler) Routes { return Routes{discovery: discovery} }

func (rt Routes) Bounded(r chi.Router) {
	r.Mount("/discovery", rt.discovery.Routes())
}

func (rt Routes) LongLived(chi.Router) {}

const maxEventBodyBytes = 32 << 10

const maxFavoriteBodyBytes = 16 << 10

const maxHistoryBodyBytes = 4 << 10

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.With(h.searchLimiter).Get("/search", h.handleSearch)
	r.With(h.suggestLimiter).Get("/suggest", h.handleSuggest)
	r.With(h.historyLimiter).Get("/search-history", h.handleSearchHistory)
	r.With(h.historyLimiter, httpserver.MaxBodySize(maxHistoryBodyBytes)).Post("/search-history", h.handleRecordSearchHistory)
	r.With(h.historyLimiter).Delete("/search-history", h.handleClearSearchHistory)
	r.With(h.eventLimiter, httpserver.MaxBodySize(maxEventBodyBytes)).Post("/events", h.handleRecordEvent)
	r.With(h.favoritesLimiter).Get("/favorites", h.handleListFavorites)
	r.With(h.favoritesLimiter, httpserver.MaxBodySize(maxFavoriteBodyBytes)).Put("/favorites", h.handleAddFavorite)
	r.With(h.favoritesLimiter, httpserver.MaxBodySize(maxFavoriteBodyBytes)).Delete("/favorites", h.handleRemoveFavorite)
	r.Group(h.contentRoutes)
	return r
}

func (h *DiscoveryHandler) contentRoutes(r chi.Router) {
	r.Use(h.contentLimiter)
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

func (h *DiscoveryHandler) PublicRoutes(r chi.Router, limit RequestLimit) {
	r.With(newClientRateLimiter(limit, h.now), httpserver.MaxBodySize(maxAuthFailureBodyBytes)).
		Post("/public/auth-failures", h.handleAuthFailure)
}
