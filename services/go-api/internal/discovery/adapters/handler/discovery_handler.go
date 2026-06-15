package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	searchSvc    *service.SearchMusicService
	clickSvc     *service.RecordClickService
	historySvc   *service.ListSearchHistoryService
	albumSvc     *service.GetAlbumTracksService
	artistSvc    *service.GetArtistContentService
}

func NewDiscoveryHandler(
	searchSvc *service.SearchMusicService,
	clickSvc *service.RecordClickService,
	historySvc *service.ListSearchHistoryService,
	albumSvc *service.GetAlbumTracksService,
	artistSvc *service.GetArtistContentService,
) *DiscoveryHandler {
	return &DiscoveryHandler{
		searchSvc:  searchSvc,
		clickSvc:   clickSvc,
		historySvc: historySvc,
		albumSvc:   albumSvc,
		artistSvc:  artistSvc,
	}
}

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/search", h.handleSearch)
	r.Get("/search-history", h.handleSearchHistory)
	r.Post("/clicks", h.handleRecordClick)
	r.Get("/albums/{provider}/{externalId}/tracks", h.handleAlbumTracks)
	r.Get("/artists/{provider}/{externalId}/top-tracks", h.handleArtistTopTracks)
	r.Get("/artists/{provider}/{externalId}/albums", h.handleArtistAlbums)
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

type DiscoverySearchResponse struct {
	Query     string              `json:"query"`
	QueryNorm string              `json:"query_norm"`
	Results   []SearchResultDTO   `json:"results"`
	Providers []ProviderStatusDTO `json:"providers"`
	Partial   bool                `json:"partial"`
	Cache     CacheDTO            `json:"cache"`
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

// --- Handlers ---

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

	result, err := h.searchSvc.Execute(r.Context(), userId, query, saveHistory)
	if err != nil {
		httputil.InternalError(w)
		return
	}

	resultDTOs := make([]SearchResultDTO, len(result.Results))
	for i, sr := range result.Results {
		sources := make([]SourceRefDTO, len(sr.Sources))
		for j, s := range sr.Sources {
			sources[j] = SourceRefDTO{
				Provider:   s.Provider.String(),
				ExternalID: s.ExternalID,
				URL:        s.URL,
			}
		}
		extras := sr.Extras
		if extras == nil {
			extras = make(map[string]any)
		}
		resultDTOs[i] = SearchResultDTO{
			Kind:       sr.Kind.String(),
			Title:      sr.Title,
			Subtitle:   sr.Subtitle,
			ImageURL:   sr.ImageURL,
			Confidence: sr.Confidence.String(),
			Sources:    sources,
			Extras:     extras,
		}
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
		Query:     q,
		QueryNorm: service.NormalizeForMatch(q),
		Results:   resultDTOs,
		Providers: providerDTOs,
		Partial:   result.Partial,
		Cache:     CacheDTO{Hit: false, FetchedAt: nil},
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
		httputil.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *DiscoveryHandler) handleAlbumTracks(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	externalID := chi.URLParam(r, "externalId")
	if provider == "" || externalID == "" {
		httputil.BadRequest(w, "provider and externalId are required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}

	if h.albumSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.albumSvc.Execute(r.Context(), provider, externalID, limit)
	if err != nil {
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

func (h *DiscoveryHandler) handleArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	externalID := chi.URLParam(r, "externalId")
	if provider == "" || externalID == "" {
		httputil.BadRequest(w, "provider and externalId are required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 5
	}

	if h.artistSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.artistSvc.GetTopTracks(r.Context(), provider, externalID, limit)
	if err != nil {
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

func (h *DiscoveryHandler) handleArtistAlbums(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	externalID := chi.URLParam(r, "externalId")
	if provider == "" || externalID == "" {
		httputil.BadRequest(w, "provider and externalId are required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 10
	}

	if h.artistSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
			Provider: provider, Status: "error", Items: []SearchResultDTO{},
		})
		return
	}

	resp, err := h.artistSvc.GetAlbums(r.Context(), provider, externalID, limit)
	if err != nil {
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}

type ContentFetchResponseDTO struct {
	Provider string            `json:"provider_name"`
	Status   string            `json:"status"`
	Items    []SearchResultDTO `json:"items"`
}

func contentFetchToDTO(resp *service.ContentFetchResponse) ContentFetchResponseDTO {
	items := make([]SearchResultDTO, len(resp.Items))
	for i, r := range resp.Items {
		sources := make([]SourceRefDTO, len(r.Sources))
		for j, s := range r.Sources {
			sources[j] = SourceRefDTO{
				Provider:   s.Provider.String(),
				ExternalID: s.ExternalID,
				URL:        s.URL,
			}
		}
		contentExtras := r.Extras
		if contentExtras == nil {
			contentExtras = make(map[string]any)
		}
		items[i] = SearchResultDTO{
			Kind:       r.Kind.String(),
			Title:      r.Title,
			Subtitle:   r.Subtitle,
			ImageURL:   r.ImageURL,
			Confidence: r.Confidence.String(),
			Sources:    sources,
			Extras:     contentExtras,
		}
	}
	return ContentFetchResponseDTO{
		Provider: resp.ProviderName,
		Status:   resp.Status.String(),
		Items:    items,
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
