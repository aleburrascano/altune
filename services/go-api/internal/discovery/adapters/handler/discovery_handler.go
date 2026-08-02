package handler

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/discovery/service/enrich"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc       *service.Service
	historySvc      *service.ListSearchHistoryService
	clearHistorySvc *service.ClearSearchHistoryService
	albumSvc        *service.GetAlbumTracksService
	artistSvc       *service.GetArtistContentService
	relatedSvc      *service.GetRelatedTracksService
	enrichSvc       *service.EnrichmentService
	suggestSvc      *service.SuggestService
	eventSvc        *service.RecordEventService
	favoritesSvc    *service.FavoritesService

	enrichers DetailEnrichers

	ownership    ports.OwnershipReader
	trackNumbers ports.TrackNumberFiller

	providerHealth providerHealthRecorder

	searchTrace searchTraceRecorder
}

type providerHealthRecorder interface {
	Record(provider, status string, latencyMs int64)
}

type searchTraceRecorder interface {
	RecordSearch(
		ctx context.Context,
		query string,
		kinds []string,
		user string,
		statuses []domain.ProviderSearchResponse,
		final []domain.SearchResult,
	)
	RecordContentFetch(
		ctx context.Context,
		kind, provider, artist, status string,
		items []domain.SearchResult,
	)
}

func (h *DiscoveryHandler) WithProviderHealth(r providerHealthRecorder) *DiscoveryHandler {
	h.providerHealth = r
	return h
}

func (h *DiscoveryHandler) WithRequestTrace(r searchTraceRecorder) *DiscoveryHandler {
	h.searchTrace = r
	return h
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
	Enrich       *service.EnrichmentService
	Suggest      *service.SuggestService
	Event        *service.RecordEventService
	Favorites    *service.FavoritesService
}

func NewDiscoveryHandler(svcs DiscoveryServices) *DiscoveryHandler {
	return &DiscoveryHandler{
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
}

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/search", h.handleSearch)
	r.Get("/suggest", h.handleSuggest)
	r.Get("/search-history", h.handleSearchHistory)
	r.Delete("/search-history", h.handleClearSearchHistory)
	r.Post("/events", h.handleRecordEvent)
	r.Get("/favorites", h.handleListFavorites)
	r.Put("/favorites", h.handleAddFavorite)
	r.Delete("/favorites", h.handleRemoveFavorite)
	r.Get("/albums/{provider}/{externalId}/tracks", h.handleAlbumTracks)
	r.Get("/artists/{provider}/{externalId}/content", h.handleArtistContent)
	r.Get("/artists/{provider}/{externalId}/top-tracks", h.handleArtistTopTracks)
	r.Get("/artists/{provider}/{externalId}/albums", h.handleArtistAlbums)
	r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)
	r.Get("/enrichment", h.handleEnrichment)
	r.Get("/enrichment/lastfm", h.handleLastFmEnrichment)
	r.Get("/enrichment/deezer", h.handleDeezerEnrichment)
	r.Get("/lyrics", h.handleLyrics)
	return r
}
