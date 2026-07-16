package handler

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/discovery/service/enrich"

	"github.com/go-chi/chi/v5"
)

// The handler is split by endpoint family within this package:
//   - discovery_handler.go    — the struct, wiring surfaces, and routes
//   - search_endpoints.go     — search/suggest/history/events + the shared wire DTOs
//   - content_endpoints.go    — album tracks / artist content / related tracks
//   - enrichment_endpoints.go — the detail-open enrichment families

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

	// enrichers holds the optional detail-open enrichment use cases, wired
	// post-construction via WithDetailEnrichers so the positional constructor
	// stays stable; a nil member degrades its endpoint to an empty DTO.
	enrichers DetailEnrichers

	// providerHealth records per-provider scatter-gather outcomes for the
	// operator console. Optional and nil-safe; recording happens at the response
	// boundary, never on the ranking path.
	providerHealth providerHealthRecorder

	// searchTrace records the full per-request story (query, user, per-provider
	// results, final list) for the operator console's discovery drill-down.
	// Optional and nil-safe; recorded at the response boundary, off the ranking path.
	searchTrace searchTraceRecorder
}

// providerHealthRecorder is the consumer-defined seam for the operator console's
// provider status board. Satisfied by the in-memory store wired at the
// composition root.
type providerHealthRecorder interface {
	Record(provider, status string, latencyMs int64)
}

// searchTraceRecorder is the consumer-defined seam for the operator console's
// discovery request drill-down. Satisfied by the in-memory request store wired at
// the composition root, which keys the trace by the request's correlation id.
type searchTraceRecorder interface {
	RecordSearch(
		ctx context.Context,
		query string,
		kinds []string,
		user string,
		statuses []domain.ProviderSearchResponse,
		final []domain.SearchResult,
	)
	// RecordContentFetch traces a detail-screen fetch (discography/top-tracks/
	// related) so the operator console can see what came up when an artist was
	// opened — not just searches.
	RecordContentFetch(
		ctx context.Context,
		kind, provider, artist, status string,
		items []domain.SearchResult,
	)
}

// WithProviderHealth attaches the optional provider-health recorder. A nil
// recorder leaves the board empty; recording never affects search behavior.
func (h *DiscoveryHandler) WithProviderHealth(r providerHealthRecorder) *DiscoveryHandler {
	h.providerHealth = r
	return h
}

// WithRequestTrace attaches the optional discovery request-trace recorder. A nil
// recorder leaves the drill-down empty; recording never affects search behavior.
func (h *DiscoveryHandler) WithRequestTrace(r searchTraceRecorder) *DiscoveryHandler {
	h.searchTrace = r
	return h
}

// DetailEnrichers bundles the optional detail-open enrichment use cases into one
// wiring surface: adding a source is one field here plus its endpoint, not a new
// With* setter wired backwards from the composition root. Any member may be nil
// (its provider isn't configured) — the endpoint then answers an empty DTO.
type DetailEnrichers struct {
	Discogs       *enrich.DiscogsEnrichmentService       // album credits/styles/labels (caps 3–6)
	DiscogsArtist *enrich.DiscogsArtistEnrichmentService // artist bio/aliases/links (cap 7)
	LastFm        *enrich.LastFmEnrichmentService        // listen popularity/tags/bio/similar (cap 3)
	Deezer        *enrich.DeezerEnrichmentService        // track audio fields + album liner (caps 7–8)
	Lyrics        *enrich.LyricsService                  // synced + plain lyrics (cap 6)
}

// WithDetailEnrichers attaches the optional detail-open enrichment use cases. Set
// at composition time; any nil member leaves its endpoint answering an empty DTO.
func (h *DiscoveryHandler) WithDetailEnrichers(e DetailEnrichers) *DiscoveryHandler {
	h.enrichers = e
	return h
}

// DiscoveryServices bundles the required (non-optional) discovery use cases the
// handler dispatches to — one named field per endpoint family. Replaces a
// 9-positional constructor: callers (the composition root, tests) name what they
// pass, and adding an endpoint is one field here, not another positional arg.
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
	}
}

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/search", h.handleSearch)
	r.Get("/suggest", h.handleSuggest)
	r.Get("/search-history", h.handleSearchHistory)
	r.Delete("/search-history", h.handleClearSearchHistory)
	r.Post("/events", h.handleRecordEvent)
	r.Get("/albums/{provider}/{externalId}/tracks", h.handleAlbumTracks)
	r.Get("/artists/{provider}/{externalId}/top-tracks", h.handleArtistTopTracks)
	r.Get("/artists/{provider}/{externalId}/albums", h.handleArtistAlbums)
	r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)
	r.Get("/enrichment", h.handleEnrichment)
	r.Get("/enrichment/discogs", h.handleDiscogsEnrichment)
	r.Get("/enrichment/discogs/artist", h.handleDiscogsArtistEnrichment)
	r.Get("/enrichment/lastfm", h.handleLastFmEnrichment)
	r.Get("/enrichment/deezer", h.handleDeezerEnrichment)
	r.Get("/lyrics", h.handleLyrics)
	return r
}
