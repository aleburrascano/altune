package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/textnorm"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc  *service.Service
	historySvc *service.ListSearchHistoryService
	albumSvc   *service.GetAlbumTracksService
	artistSvc  *service.GetArtistContentService
	relatedSvc *service.GetRelatedTracksService
	enrichSvc  *service.EnrichmentService
	suggestSvc *service.SuggestService
	eventSvc   *service.RecordEventService

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
	Search  *service.Service
	History *service.ListSearchHistoryService
	Album   *service.GetAlbumTracksService
	Artist  *service.GetArtistContentService
	Related *service.GetRelatedTracksService
	Enrich  *service.EnrichmentService
	Suggest *service.SuggestService
	Event   *service.RecordEventService
}

func NewDiscoveryHandler(svcs DiscoveryServices) *DiscoveryHandler {
	return &DiscoveryHandler{
		searchSvc:  svcs.Search,
		historySvc: svcs.History,
		albumSvc:   svcs.Album,
		artistSvc:  svcs.Artist,
		relatedSvc: svcs.Related,
		enrichSvc:  svcs.Enrich,
		suggestSvc: svcs.Suggest,
		eventSvc:   svcs.Event,
	}
}

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/search", h.handleSearch)
	r.Get("/suggest", h.handleSuggest)
	r.Get("/search-history", h.handleSearchHistory)
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

// --- DTOs ---

type SearchResultDTO struct {
	Kind          string         `json:"kind"`
	Title         string         `json:"title"`
	Subtitle      string         `json:"subtitle,omitempty"`
	ImageURL      string         `json:"image_url,omitempty"`
	ArtworkSource string         `json:"artwork_source,omitempty"`
	Confidence    string         `json:"confidence"`
	Sources       []SourceRefDTO `json:"sources"`
	Extras        map[string]any `json:"extras"`
}

type SourceRefDTO struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

type ProviderStatusDTO struct {
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	LatencyMs   int64  `json:"latency_ms"`
	ResultCount int    `json:"result_count"`
}

type RelatedGroupDTO struct {
	Relationship string            `json:"relationship"`
	RelatedTo    string            `json:"related_to"`
	Items        []SearchResultDTO `json:"items"`
}

type DiscoverySearchResponse struct {
	Query          string              `json:"query"`
	QueryNorm      string              `json:"query_norm"`
	Results        []SearchResultDTO   `json:"results"`
	Providers      []ProviderStatusDTO `json:"providers"`
	Partial        bool                `json:"partial"`
	Cache          CacheDTO            `json:"cache"`
	CorrectedQuery string              `json:"corrected_query,omitempty"`
	OriginalQuery  string              `json:"original_query,omitempty"`
	Related        []RelatedGroupDTO   `json:"related,omitempty"`
}

type CacheDTO struct {
	Hit       bool    `json:"hit"`
	FetchedAt *string `json:"fetched_at"`
}

type SearchHistoryItemDTO struct {
	Query      string `json:"query"`
	QueryNorm  string `json:"query_norm"`
	ExecutedAt string `json:"executed_at"`
}

type DiscoverySearchHistoryResponse struct {
	Items []SearchHistoryItemDTO `json:"items"`
	Total int                    `json:"total"`
}

type DiscoveryEventRequest struct {
	Type      string         `json:"type"`
	QueryNorm string         `json:"query_norm"`
	Payload   map[string]any `json:"payload"`
}

type SuggestionDTO struct {
	Text       string `json:"text"`
	Kind       string `json:"kind"`
	Popularity int64  `json:"popularity"`
}

type SuggestResponse struct {
	Suggestions []SuggestionDTO `json:"suggestions"`
}

// --- Handlers ---

func (h *DiscoveryHandler) handleSuggest(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httputil.BadRequest(w, "q parameter is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 10 {
		limit = 5
	}

	entries, err := h.suggestSvc.Execute(r.Context(), q, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "suggest failed", "error", err, "query", q)
		httputil.InternalError(w)
		return
	}

	dtos := make([]SuggestionDTO, len(entries))
	for i, e := range entries {
		dtos[i] = SuggestionDTO{
			Text:       e.Term,
			Kind:       string(e.Kind),
			Popularity: e.Popularity,
		}
	}
	httputil.WriteJSON(w, http.StatusOK, SuggestResponse{Suggestions: dtos})
}

func (h *DiscoveryHandler) executeSearch(
	ctx context.Context,
	userId shared.UserId,
	query *domain.SearchQuery,
	saveHistory bool,
) (*service.SearchOutput, error) {
	return h.searchSvc.Execute(ctx, userId, query, saveHistory)
}

func (h *DiscoveryHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		httputil.BadRequest(w, "q parameter is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	kinds, err := parseKinds(r.URL.Query().Get("kinds"))
	if err != nil {
		httputil.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	saveHistory := true
	if r.URL.Query().Get("save_history") == "false" {
		saveHistory = false
	}

	query, err := domain.NewSearchQuery(q, "", kinds, limit)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	result, err := h.executeSearch(r.Context(), userId, query, saveHistory)
	if err != nil {
		slog.ErrorContext(r.Context(), "search failed", "error", err, "query", q)
		httputil.InternalError(w)
		return
	}

	if h.providerHealth != nil {
		for _, ps := range result.ProviderStatuses {
			h.providerHealth.Record(ps.Provider.String(), ps.Status.String(), ps.LatencyMs)
		}
	}

	if h.searchTrace != nil {
		h.searchTrace.RecordSearch(r.Context(), q, kindNames(kinds), userId.String(), result.ProviderStatuses, result.Results)
	}

	httputil.WriteJSON(w, searchStatusCode(result.ProviderStatuses), DiscoverySearchResponse{
		Query:          q,
		QueryNorm:      textnorm.NormalizeForMatch(q),
		Results:        searchResultsToDTOs(result.Results),
		Providers:      providerStatusesToDTOs(result.ProviderStatuses),
		Partial:        result.Partial,
		Cache:          CacheDTO{Hit: false, FetchedAt: nil},
		CorrectedQuery: result.CorrectedQuery,
		OriginalQuery:  result.OriginalQuery,
		Related:        relatedGroupsToDTOs(result.Related),
	})
}

func (h *DiscoveryHandler) handleSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	entries, err := h.historySvc.Execute(r.Context(), userId, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "search history failed", "error", err)
		httputil.InternalError(w)
		return
	}

	items := make([]SearchHistoryItemDTO, len(entries))
	for i, e := range entries {
		items[i] = SearchHistoryItemDTO{
			Query:      e.Query,
			QueryNorm:  e.QueryNorm,
			ExecutedAt: e.ExecutedAt.Format("2006-01-02T15:04:05.000Z"),
		}
	}

	httputil.WriteJSON(w, http.StatusOK, DiscoverySearchHistoryResponse{
		Items: items,
		Total: len(items),
	})
}

func (h *DiscoveryHandler) handleRecordEvent(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var req DiscoveryEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	eventType := domain.ParseEventType(req.Type)
	if eventType == domain.EventTypeUnknown {
		httputil.BadRequest(w, "invalid event type")
		return
	}

	if h.eventSvc == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	input := service.RecordEventInput{
		Type:      eventType,
		QueryNorm: req.QueryNorm,
		Payload:   req.Payload,
	}
	if err := h.eventSvc.Execute(r.Context(), userId, input); err != nil {
		slog.ErrorContext(r.Context(), "record event failed", "error", err)
		httputil.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func validateContentParams(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	provider := chi.URLParam(r, "provider")
	externalID := chi.URLParam(r, "externalId")
	if provider == "" || externalID == "" {
		httputil.BadRequest(w, "provider and externalId are required")
		return "", "", false
	}
	if len(externalID) > 256 {
		httputil.BadRequest(w, "externalId too long")
		return "", "", false
	}
	return provider, externalID, true
}

func (h *DiscoveryHandler) handleAlbumTracks(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}
	albumTitle := strings.TrimSpace(r.URL.Query().Get("title"))
	albumArtist := strings.TrimSpace(r.URL.Query().Get("artist"))

	if h.albumSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.albumSvc.Execute(r.Context(), provider, externalID, albumTitle, albumArtist, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "get album tracks failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

func (h *DiscoveryHandler) handleArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 5
	} else if limit > 50 {
		limit = 50
	}

	if h.artistSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.artistSvc.GetTopTracks(r.Context(), provider, externalID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "get artist top tracks failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	if h.searchTrace != nil {
		h.searchTrace.RecordContentFetch(r.Context(), "top_tracks", provider, "", resp.Status.String(), resp.Items)
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

func (h *DiscoveryHandler) handleArtistAlbums(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}
	artistName := strings.TrimSpace(r.URL.Query().Get("name"))

	if h.artistSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.artistSvc.GetAlbums(r.Context(), provider, externalID, artistName, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "get artist albums failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	if h.searchTrace != nil {
		h.searchTrace.RecordContentFetch(r.Context(), "albums", provider, artistName, resp.Status.String(), resp.Items)
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

func (h *DiscoveryHandler) handleRelatedTracks(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	} else if limit > 50 {
		limit = 50
	}

	if h.relatedSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.relatedSvc.Execute(r.Context(), provider, externalID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "get related tracks failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

// handleEnrichment serves MusicBrainz detail-open enrichment for one entity.
// Always 200 with the DTO (or an empty DTO) on the happy path — degradation is
// the service's concern; only request-shape problems are 4xx.
func (h *DiscoveryHandler) handleEnrichment(w http.ResponseWriter, r *http.Request) {
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		httputil.BadRequest(w, "kind is required")
		return
	}
	kind, err := domain.ParseResultKind(kindStr)
	if err != nil {
		httputil.BadRequest(w, "invalid kind")
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	mbid := strings.TrimSpace(r.URL.Query().Get("mbid"))
	if title == "" && mbid == "" {
		httputil.BadRequest(w, "title or mbid is required")
		return
	}

	if h.enrichSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, enrichmentToDTO(domain.EmptyEnrichment()))
		return
	}

	e, err := h.enrichSvc.Execute(r.Context(), kind, title, subtitle, mbid)
	if err != nil {
		slog.ErrorContext(r.Context(), "enrichment failed",
			"error", err, "kind", kindStr, "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, enrichmentToDTO(e))
}

type EnrichmentResponseDTO struct {
	MBID           string            `json:"mbid"`
	Genres         []string          `json:"genres"`
	Year           int               `json:"year"`
	Rating         float64           `json:"rating"`
	RatingVotes    int               `json:"rating_votes"`
	PrimaryType    string            `json:"primary_type"`
	SecondaryTypes []string          `json:"secondary_types"`
	ExternalIDs    map[string]string `json:"external_ids"`
	ArtworkURL     string            `json:"artwork_url"`
}

func enrichmentToDTO(e domain.MBEnrichment) EnrichmentResponseDTO {
	genres := e.Genres
	if genres == nil {
		genres = []string{}
	}
	secondary := e.SecondaryTypes
	if secondary == nil {
		secondary = []string{}
	}
	ids := e.ExternalIDs
	if ids == nil {
		ids = map[string]string{}
	}
	return EnrichmentResponseDTO{
		MBID:           e.MBID,
		Genres:         genres,
		Year:           e.Year,
		Rating:         e.Rating,
		RatingVotes:    e.RatingVotes,
		PrimaryType:    e.PrimaryType,
		SecondaryTypes: secondary,
		ExternalIDs:    ids,
		ArtworkURL:     e.ArtworkURL,
	}
}

// handleDiscogsEnrichment serves Discogs detail-open album enrichment: credits,
// styles, label/catalog, companies, and community signal (docs/providers/discogs.md
// caps 3–6). Album-scoped — resolved from `album` + `artist`. Always 200 with the
// DTO (or an empty DTO); only request-shape problems are 4xx.
func (h *DiscoveryHandler) handleDiscogsEnrichment(w http.ResponseWriter, r *http.Request) {
	album := strings.TrimSpace(r.URL.Query().Get("album"))
	artist := strings.TrimSpace(r.URL.Query().Get("artist"))
	if album == "" {
		httputil.BadRequest(w, "album is required")
		return
	}

	if h.enrichers.Discogs == nil {
		httputil.WriteJSON(w, http.StatusOK, discogsEnrichmentToDTO(domain.EmptyDiscogsEnrichment()))
		return
	}

	e, err := h.enrichers.Discogs.Execute(r.Context(), artist, album)
	if err != nil {
		slog.ErrorContext(r.Context(), "discogs enrichment failed",
			"error", err, "album", album, "artist", artist)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, discogsEnrichmentToDTO(e))
}

type DiscogsCreditDTO struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type DiscogsLabelDTO struct {
	Name  string `json:"name"`
	Catno string `json:"catno"`
}

type DiscogsCompanyDTO struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type DiscogsCommunityDTO struct {
	Have   int     `json:"have"`
	Want   int     `json:"want"`
	Rating float64 `json:"rating"`
	Votes  int     `json:"votes"`
}

type DiscogsEnrichmentResponseDTO struct {
	MasterID  int                 `json:"master_id"`
	Genres    []string            `json:"genres"`
	Styles    []string            `json:"styles"`
	Year      int                 `json:"year"`
	Credits   []DiscogsCreditDTO  `json:"credits"`
	Labels    []DiscogsLabelDTO   `json:"labels"`
	Formats   []string            `json:"formats"`
	Country   string              `json:"country"`
	Companies []DiscogsCompanyDTO `json:"companies"`
	Community DiscogsCommunityDTO `json:"community"`
}

func discogsEnrichmentToDTO(e domain.DiscogsEnrichment) DiscogsEnrichmentResponseDTO {
	credits := make([]DiscogsCreditDTO, len(e.Credits))
	for i, c := range e.Credits {
		credits[i] = DiscogsCreditDTO{Name: c.Name, Role: c.Role}
	}
	labels := make([]DiscogsLabelDTO, len(e.Labels))
	for i, l := range e.Labels {
		labels[i] = DiscogsLabelDTO{Name: l.Name, Catno: l.Catno}
	}
	companies := make([]DiscogsCompanyDTO, len(e.Companies))
	for i, c := range e.Companies {
		companies[i] = DiscogsCompanyDTO{Name: c.Name, Role: c.Role}
	}
	genres := e.Genres
	if genres == nil {
		genres = []string{}
	}
	styles := e.Styles
	if styles == nil {
		styles = []string{}
	}
	formats := e.Formats
	if formats == nil {
		formats = []string{}
	}
	return DiscogsEnrichmentResponseDTO{
		MasterID:  e.MasterID,
		Genres:    genres,
		Styles:    styles,
		Year:      e.Year,
		Credits:   credits,
		Labels:    labels,
		Formats:   formats,
		Country:   e.Country,
		Companies: companies,
		Community: DiscogsCommunityDTO{
			Have:   e.Community.Have,
			Want:   e.Community.Want,
			Rating: e.Community.Rating,
			Votes:  e.Community.Votes,
		},
	}
}

// handleDiscogsArtistEnrichment serves Discogs detail-open artist enrichment:
// bio, name history, group/member links, and external links (cap 7). Resolved
// from `name`. Always 200 with the DTO (or an empty DTO); only request-shape
// problems are 4xx.
func (h *DiscoveryHandler) handleDiscogsArtistEnrichment(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		httputil.BadRequest(w, "name is required")
		return
	}

	if h.enrichers.DiscogsArtist == nil {
		httputil.WriteJSON(w, http.StatusOK, discogsArtistEnrichmentToDTO(domain.EmptyDiscogsArtistEnrichment()))
		return
	}

	e, err := h.enrichers.DiscogsArtist.Execute(r.Context(), name)
	if err != nil {
		slog.ErrorContext(r.Context(), "discogs artist enrichment failed",
			"error", err, "name", name)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, discogsArtistEnrichmentToDTO(e))
}

type DiscogsLinkDTO struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type DiscogsArtistEnrichmentResponseDTO struct {
	ArtistID       int              `json:"artist_id"`
	Profile        string           `json:"profile"`
	RealName       string           `json:"real_name"`
	Aliases        []string         `json:"aliases"`
	NameVariations []string         `json:"name_variations"`
	Members        []string         `json:"members"`
	Groups         []string         `json:"groups"`
	Links          []DiscogsLinkDTO `json:"links"`
}

func discogsArtistEnrichmentToDTO(e domain.DiscogsArtistEnrichment) DiscogsArtistEnrichmentResponseDTO {
	links := make([]DiscogsLinkDTO, len(e.Links))
	for i, l := range e.Links {
		links[i] = DiscogsLinkDTO{Label: l.Label, URL: l.URL}
	}
	return DiscogsArtistEnrichmentResponseDTO{
		ArtistID:       e.ArtistID,
		Profile:        e.Profile,
		RealName:       e.RealName,
		Aliases:        nonNilStrings(e.Aliases),
		NameVariations: nonNilStrings(e.NameVariations),
		Members:        nonNilStrings(e.Members),
		Groups:         nonNilStrings(e.Groups),
		Links:          links,
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// handleLastFmEnrichment serves Last.fm detail-open enrichment for one entity:
// listen-based popularity, weighted tags, bio, and (for artists) similar
// artists (docs/providers/lastfm.md cap 3). Kind-dispatched from `kind` +
// `title` + `subtitle`. Always 200 with the DTO (or an empty DTO); only
// request-shape problems are 4xx.
func (h *DiscoveryHandler) handleLastFmEnrichment(w http.ResponseWriter, r *http.Request) {
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		httputil.BadRequest(w, "kind is required")
		return
	}
	kind, err := domain.ParseResultKind(kindStr)
	if err != nil {
		httputil.BadRequest(w, "invalid kind")
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	if h.enrichers.LastFm == nil {
		httputil.WriteJSON(w, http.StatusOK, lastfmEnrichmentToDTO(domain.EmptyLastFmEnrichment()))
		return
	}

	e, err := h.enrichers.LastFm.Execute(r.Context(), kind, title, subtitle)
	if err != nil {
		slog.ErrorContext(r.Context(), "lastfm enrichment failed",
			"error", err, "kind", kindStr, "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, lastfmEnrichmentToDTO(e))
}

type LastFmEnrichmentResponseDTO struct {
	MBID      string   `json:"mbid"`
	Listeners int64    `json:"listeners"`
	Playcount int64    `json:"playcount"`
	Tags      []string `json:"tags"`
	Bio       string   `json:"bio"`
	Similar   []string `json:"similar"`
	Duration  int      `json:"duration"`
	Album     string   `json:"album"`
}

func lastfmEnrichmentToDTO(e domain.LastFmEnrichment) LastFmEnrichmentResponseDTO {
	return LastFmEnrichmentResponseDTO{
		MBID:      e.MBID,
		Listeners: e.Listeners,
		Playcount: e.Playcount,
		Tags:      nonNilStrings(e.Tags),
		Bio:       e.Bio,
		Similar:   nonNilStrings(e.Similar),
		Duration:  e.Duration,
		Album:     e.Album,
	}
}

// handleDeezerEnrichment serves Deezer detail-open enrichment for one track or
// album: the audio fields (bpm/gain) + explicit flag for tracks, and label /
// genres / barcode / record type for albums (docs/providers/deezer.md caps 7–8).
// Kind-dispatched from `kind` + `title` + `subtitle`. Always 200 with the DTO
// (or an empty DTO); only request-shape problems are 4xx.
func (h *DiscoveryHandler) handleDeezerEnrichment(w http.ResponseWriter, r *http.Request) {
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		httputil.BadRequest(w, "kind is required")
		return
	}
	kind, err := domain.ParseResultKind(kindStr)
	if err != nil {
		httputil.BadRequest(w, "invalid kind")
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	if h.enrichers.Deezer == nil {
		httputil.WriteJSON(w, http.StatusOK, deezerEnrichmentToDTO(domain.EmptyDeezerEnrichment()))
		return
	}

	e, err := h.enrichers.Deezer.Execute(r.Context(), kind, title, subtitle)
	if err != nil {
		slog.ErrorContext(r.Context(), "deezer enrichment failed",
			"error", err, "kind", kindStr, "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, deezerEnrichmentToDTO(e))
}

type DeezerEnrichmentResponseDTO struct {
	BPM        int      `json:"bpm"`
	Gain       float64  `json:"gain"`
	Explicit   bool     `json:"explicit"`
	Label      string   `json:"label"`
	Genres     []string `json:"genres"`
	UPC        string   `json:"upc"`
	RecordType string   `json:"record_type"`
}

func deezerEnrichmentToDTO(e domain.DeezerEnrichment) DeezerEnrichmentResponseDTO {
	return DeezerEnrichmentResponseDTO{
		BPM:        e.BPM,
		Gain:       e.Gain,
		Explicit:   e.Explicit,
		Label:      e.Label,
		Genres:     nonNilStrings(e.Genres),
		UPC:        e.UPC,
		RecordType: e.RecordType,
	}
}

// handleLyrics serves Deezer lyrics for one track: the full plain text, the
// time-synced lines (when available), the songwriter credits, and the copyright
// line (docs/providers/deezer.md cap 6). Identified by `title` (track) +
// `subtitle` (artist). Always 200 with the DTO (or an empty DTO); only
// request-shape problems are 4xx. Lyrics apply to tracks only — no kind param.
func (h *DiscoveryHandler) handleLyrics(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	if h.enrichers.Lyrics == nil {
		httputil.WriteJSON(w, http.StatusOK, lyricsToDTO(domain.EmptyDeezerLyrics()))
		return
	}

	l, err := h.enrichers.Lyrics.Execute(r.Context(), title, subtitle)
	if err != nil {
		slog.ErrorContext(r.Context(), "lyrics fetch failed", "error", err, "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, lyricsToDTO(l))
}

type LyricsResponseDTO struct {
	Plain       string          `json:"plain"`
	SyncedLines []SyncedLineDTO `json:"synced_lines"`
	Writers     []string        `json:"writers"`
	Copyright   string          `json:"copyright"`
}

type SyncedLineDTO struct {
	Timecode     string `json:"timecode"`
	Line         string `json:"line"`
	Milliseconds int64  `json:"milliseconds"`
	Duration     int64  `json:"duration"`
}

func lyricsToDTO(l domain.DeezerLyrics) LyricsResponseDTO {
	lines := make([]SyncedLineDTO, len(l.SyncedLines))
	for i, ln := range l.SyncedLines {
		lines[i] = SyncedLineDTO{
			Timecode:     ln.Timecode,
			Line:         ln.Line,
			Milliseconds: ln.Milliseconds,
			Duration:     ln.Duration,
		}
	}
	return LyricsResponseDTO{
		Plain:       l.Plain,
		SyncedLines: lines,
		Writers:     nonNilStrings(l.Writers),
		Copyright:   l.Copyright,
	}
}

type ContentFetchResponseDTO struct {
	Provider string            `json:"provider_name"`
	Status   string            `json:"status"`
	Items    []SearchResultDTO `json:"items"`
}

func contentFetchToDTO(resp *service.ContentFetchResponse) ContentFetchResponseDTO {
	items := make([]SearchResultDTO, len(resp.Items))
	for i, r := range resp.Items {
		items[i] = searchResultToDTO(r)
	}
	return ContentFetchResponseDTO{
		Provider: resp.ProviderName,
		Status:   resp.Status.String(),
		Items:    items,
	}
}

func searchResultToDTO(sr domain.SearchResult) SearchResultDTO {
	sources := make([]SourceRefDTO, len(sr.Sources))
	for i, s := range sr.Sources {
		sources[i] = SourceRefDTO{
			Provider:   s.Provider.String(),
			ExternalID: s.ExternalID,
			URL:        s.URL,
		}
	}
	extras := sr.Extras
	if extras == nil {
		extras = make(map[string]any)
	}
	return SearchResultDTO{
		Kind:          sr.Kind.String(),
		Title:         sr.Title,
		Subtitle:      sr.Subtitle,
		ImageURL:      sr.ImageURL,
		ArtworkSource: sr.ArtworkSource,
		Confidence:    sr.Confidence.String(),
		Sources:       sources,
		Extras:        extras,
	}
}

// searchResultsToDTOs maps a slice of domain results to wire DTOs.
func searchResultsToDTOs(results []domain.SearchResult) []SearchResultDTO {
	dtos := make([]SearchResultDTO, len(results))
	for i, sr := range results {
		dtos[i] = searchResultToDTO(sr)
	}
	return dtos
}

// relatedGroupsToDTOs maps the related-tracks groups to wire DTOs. Returns nil
// (not an empty slice) when there are no groups, preserving the response's
// omitempty behavior for the related block.
func relatedGroupsToDTOs(groups []domain.RelatedGroup) []RelatedGroupDTO {
	if len(groups) == 0 {
		return nil
	}
	dtos := make([]RelatedGroupDTO, 0, len(groups))
	for _, g := range groups {
		dtos = append(dtos, RelatedGroupDTO{
			Relationship: g.Relationship,
			RelatedTo:    g.RelatedTo,
			Items:        searchResultsToDTOs(g.Items),
		})
	}
	return dtos
}

// providerStatusesToDTOs maps per-provider scatter-gather outcomes to wire DTOs.
func providerStatusesToDTOs(statuses []domain.ProviderSearchResponse) []ProviderStatusDTO {
	dtos := make([]ProviderStatusDTO, len(statuses))
	for i, ps := range statuses {
		dtos[i] = ProviderStatusDTO{
			Provider:    ps.Provider.String(),
			Status:      ps.Status.String(),
			LatencyMs:   ps.LatencyMs,
			ResultCount: ps.ResultCount,
		}
	}
	return dtos
}

// searchStatusCode is 503 when a non-empty scatter had every provider fail, else
// 200 — an all-error fan-out is surfaced as service-unavailable per AC.
func searchStatusCode(statuses []domain.ProviderSearchResponse) int {
	if len(statuses) == 0 {
		return http.StatusOK
	}
	for _, ps := range statuses {
		if ps.Status == domain.ProviderStatusOK {
			return http.StatusOK
		}
	}
	return http.StatusServiceUnavailable
}

// kindNames returns the requested kinds as a stable, sorted slice of names for
// the operator console's request trace.
func kindNames(kinds map[domain.ResultKind]bool) []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k.String())
	}
	sort.Strings(out)
	return out
}

func parseKinds(csv string) (map[domain.ResultKind]bool, error) {
	if csv == "" {
		return map[domain.ResultKind]bool{
			domain.ResultKindTrack:  true,
			domain.ResultKindAlbum:  true,
			domain.ResultKindArtist: true,
		}, nil
	}
	kinds := make(map[domain.ResultKind]bool)
	var invalid []string
	for _, s := range strings.Split(csv, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k, err := domain.ParseResultKind(s)
		if err != nil {
			invalid = append(invalid, s)
		} else {
			kinds[k] = true
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid kinds: %s", strings.Join(invalid, ", "))
	}
	if len(kinds) == 0 {
		return map[domain.ResultKind]bool{
			domain.ResultKindTrack:  true,
			domain.ResultKindAlbum:  true,
			domain.ResultKindArtist: true,
		}, nil
	}
	return kinds, nil
}
