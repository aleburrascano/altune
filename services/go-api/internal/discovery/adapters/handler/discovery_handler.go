package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc  *service.Service
	clickSvc   *service.RecordClickService
	historySvc *service.ListSearchHistoryService
	albumSvc   *service.GetAlbumTracksService
	artistSvc  *service.GetArtistContentService
	relatedSvc *service.GetRelatedTracksService
	enrichSvc  *service.EnrichmentService
	suggestSvc *service.SuggestService
	eventSvc   *service.RecordEventService
}

func NewDiscoveryHandler(
	searchSvc *service.Service,
	clickSvc *service.RecordClickService,
	historySvc *service.ListSearchHistoryService,
	albumSvc *service.GetAlbumTracksService,
	artistSvc *service.GetArtistContentService,
	relatedSvc *service.GetRelatedTracksService,
	enrichSvc *service.EnrichmentService,
	suggestSvc *service.SuggestService,
	eventSvc *service.RecordEventService,
) *DiscoveryHandler {
	return &DiscoveryHandler{
		searchSvc:  searchSvc,
		clickSvc:   clickSvc,
		historySvc: historySvc,
		albumSvc:   albumSvc,
		artistSvc:  artistSvc,
		relatedSvc: relatedSvc,
		enrichSvc:  enrichSvc,
		suggestSvc: suggestSvc,
		eventSvc:   eventSvc,
	}
}

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/search", h.handleSearch)
	r.Get("/suggest", h.handleSuggest)
	r.Get("/search-history", h.handleSearchHistory)
	r.Post("/clicks", h.handleRecordClick)
	r.Post("/events", h.handleRecordEvent)
	r.Get("/albums/{provider}/{externalId}/tracks", h.handleAlbumTracks)
	r.Get("/artists/{provider}/{externalId}/top-tracks", h.handleArtistTopTracks)
	r.Get("/artists/{provider}/{externalId}/albums", h.handleArtistAlbums)
	r.Get("/tracks/{provider}/{externalId}/related", h.handleRelatedTracks)
	r.Get("/enrichment", h.handleEnrichment)
	return r
}

// --- DTOs ---

type SearchResultDTO struct {
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Subtitle   string         `json:"subtitle,omitempty"`
	ImageURL   string         `json:"image_url,omitempty"`
	Confidence string         `json:"confidence"`
	Sources    []SourceRefDTO `json:"sources"`
	Extras     map[string]any `json:"extras"`
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
	SuggestedQuery string              `json:"suggested_query,omitempty"`
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

type DiscoveryClickRequest struct {
	QueryNorm  string `json:"query_norm"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle"`
	Position   int    `json:"position"`
	Confidence string `json:"confidence"`
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

	resultDTOs := make([]SearchResultDTO, len(result.Results))
	for i, sr := range result.Results {
		resultDTOs[i] = searchResultToDTO(sr)
	}

	var relatedDTOs []RelatedGroupDTO
	for _, g := range result.Related {
		items := make([]SearchResultDTO, len(g.Items))
		for i, sr := range g.Items {
			items[i] = searchResultToDTO(sr)
		}
		relatedDTOs = append(relatedDTOs, RelatedGroupDTO{
			Relationship: g.Relationship,
			RelatedTo:    g.RelatedTo,
			Items:        items,
		})
	}

	providerDTOs := make([]ProviderStatusDTO, len(result.ProviderStatuses))
	for i, ps := range result.ProviderStatuses {
		providerDTOs[i] = ProviderStatusDTO{
			Provider:    ps.Provider.String(),
			Status:      ps.Status.String(),
			LatencyMs:   ps.LatencyMs,
			ResultCount: ps.ResultCount,
		}
	}

	status := http.StatusOK
	if len(result.ProviderStatuses) > 0 {
		allFailed := true
		for _, ps := range result.ProviderStatuses {
			if ps.Status == domain.ProviderStatusOK {
				allFailed = false
				break
			}
		}
		if allFailed {
			status = http.StatusServiceUnavailable
		}
	}

	httputil.WriteJSON(w, status, DiscoverySearchResponse{
		Query:          q,
		QueryNorm:      service.NormalizeForMatch(q),
		Results:        resultDTOs,
		Providers:      providerDTOs,
		Partial:        result.Partial,
		Cache:          CacheDTO{Hit: false, FetchedAt: nil},
		CorrectedQuery: result.CorrectedQuery,
		OriginalQuery:  result.OriginalQuery,
		SuggestedQuery: result.SuggestedQuery,
		Related:        relatedDTOs,
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

func (h *DiscoveryHandler) handleRecordClick(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	var req DiscoveryClickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	resultKind, err := domain.ParseResultKind(req.Kind)
	if err != nil {
		httputil.BadRequest(w, "invalid kind")
		return
	}

	confidence, err := domain.ParseConfidence(req.Confidence)
	if err != nil {
		httputil.BadRequest(w, "invalid confidence")
		return
	}

	input := service.RecordClickInput{
		QueryNorm:      req.QueryNorm,
		ResultKind:     resultKind,
		ResultTitle:    req.Title,
		ResultSubtitle: req.Subtitle,
		Position:       req.Position,
		Confidence:     confidence,
	}

	if err := h.clickSvc.Execute(r.Context(), userId, input); err != nil {
		slog.ErrorContext(r.Context(), "record click failed", "error", err)
		httputil.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
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
		Kind:       sr.Kind.String(),
		Title:      sr.Title,
		Subtitle:   sr.Subtitle,
		ImageURL:   sr.ImageURL,
		Confidence: sr.Confidence.String(),
		Sources:    sources,
		Extras:     extras,
	}
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
