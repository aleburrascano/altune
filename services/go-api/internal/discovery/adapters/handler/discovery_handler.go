package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type DiscoveryHandler struct {
	searchSvc  *service.SearchMusicService
	clickSvc   *service.RecordClickService
	historySvc *service.ListSearchHistoryService
}

func NewDiscoveryHandler(
	searchSvc *service.SearchMusicService,
	clickSvc *service.RecordClickService,
	historySvc *service.ListSearchHistoryService,
) *DiscoveryHandler {
	return &DiscoveryHandler{
		searchSvc:  searchSvc,
		clickSvc:   clickSvc,
		historySvc: historySvc,
	}
}

func (h *DiscoveryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/search", h.handleSearch)
	r.Get("/search-history", h.handleSearchHistory)
	r.Post("/clicks", h.handleRecordClick)
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
	Extras     map[string]any `json:"extras,omitempty"`
}

type SourceRefDTO struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

type ProviderStatusDTO struct {
	Provider string `json:"provider"`
	Status   string `json:"status"`
}

type DiscoverySearchResponse struct {
	Results   []SearchResultDTO   `json:"results"`
	Providers []ProviderStatusDTO `json:"providers"`
}

type SearchHistoryItemDTO struct {
	ID        uuid.UUID `json:"id"`
	Query     string    `json:"query"`
	QueryNorm string    `json:"query_norm"`
	ExecutedAt time.Time `json:"executed_at"`
}

type DiscoverySearchHistoryResponse struct {
	Items []SearchHistoryItemDTO `json:"items"`
}

type DiscoveryClickRequest struct {
	QueryNorm      string   `json:"query_norm"`
	ResultTitle    string   `json:"result_title"`
	ResultSubtitle string   `json:"result_subtitle"`
	Position       int      `json:"position"`
	Confidence     string   `json:"confidence"`
}

// --- Handlers ---

func (h *DiscoveryHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())

	q := r.URL.Query().Get("q")
	if q == "" {
		httputil.BadRequest(w, "q parameter is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}

	query, err := domain.NewSearchQuery(q, "", kinds, limit)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	result, err := h.searchSvc.Execute(r.Context(), userId, query)
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
		resultDTOs[i] = SearchResultDTO{
			Kind:       sr.Kind.String(),
			Title:      sr.Title,
			Subtitle:   sr.Subtitle,
			ImageURL:   sr.ImageURL,
			Confidence: sr.Confidence.String(),
			Sources:    sources,
			Extras:     sr.Extras,
		}
	}

	providerDTOs := make([]ProviderStatusDTO, len(result.ProviderStatuses))
	for i, ps := range result.ProviderStatuses {
		providerDTOs[i] = ProviderStatusDTO{
			Provider: ps.Provider.String(),
			Status:   ps.Status.String(),
		}
	}

	httputil.WriteJSON(w, http.StatusOK, DiscoverySearchResponse{
		Results:   resultDTOs,
		Providers: providerDTOs,
	})
}

func (h *DiscoveryHandler) handleSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	entries, err := h.historySvc.Execute(r.Context(), userId, limit)
	if err != nil {
		httputil.InternalError(w)
		return
	}

	items := make([]SearchHistoryItemDTO, len(entries))
	for i, e := range entries {
		items[i] = SearchHistoryItemDTO{
			ID:         e.ID,
			Query:      e.Query,
			QueryNorm:  e.QueryNorm,
			ExecutedAt: e.ExecutedAt,
		}
	}

	httputil.WriteJSON(w, http.StatusOK, DiscoverySearchHistoryResponse{Items: items})
}

func (h *DiscoveryHandler) handleRecordClick(w http.ResponseWriter, r *http.Request) {
	userId := auth.MustUserID(r.Context())

	var req DiscoveryClickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return
	}

	input := service.RecordClickInput{
		QueryNorm:      req.QueryNorm,
		ResultTitle:    req.ResultTitle,
		ResultSubtitle: req.ResultSubtitle,
		Position:       req.Position,
	}

	if err := h.clickSvc.Execute(r.Context(), userId, input); err != nil {
		httputil.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
