package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/textnorm"
)

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

	limit := limitResetOnOverflow(r, 5, 10)

	entries, err := h.suggestSvc.Execute(r.Context(), q, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "suggest failed", "error", err)
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

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	kinds, err := parseKinds(r.URL.Query().Get("kinds"))
	if err != nil {
		httputil.WriteError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	saveHistory := true
	if r.URL.Query().Get("save_history") == "false" {
		saveHistory = false
	}

	query, err := domain.NewPagedSearchQuery(q, kinds, limit, offset)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	result, err := h.searchSvc.Execute(r.Context(), userId, query, saveHistory)
	if err != nil {
		slog.ErrorContext(r.Context(), "search failed", "error", err)
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

	results := searchResultsToDTOs(result.Results)
	topResult, sections := blendedSlateToDTOs(result.Slate)
	h.stampOwnership(r.Context(), userId, ownershipTargets(results, topResult, sections)...)

	httputil.WriteJSON(w, searchStatusCode(result.ProviderStatuses), DiscoverySearchResponse{
		Query:          q,
		QueryNorm:      textnorm.NormalizeForMatch(q),
		SearchID:       result.SearchId,
		Results:        results,
		TopResult:      topResult,
		Sections:       sections,
		Providers:      providerStatusesToDTOs(result.ProviderStatuses),
		Partial:        result.Partial,
		Exploration:    result.Explored,
		Cache:          CacheDTO{Hit: false, FetchedAt: nil},
		CorrectedQuery: result.CorrectedQuery,
		OriginalQuery:  result.OriginalQuery,
		Related:        relatedGroupsToDTOs(result.Related),
		Total:          result.Total,
		Offset:         result.Offset,
		HasMore:        result.HasMore,
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
			ExecutedAt: e.ExecutedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		}
	}

	httputil.WriteJSON(w, http.StatusOK, DiscoverySearchHistoryResponse{
		Items: items,
		Total: len(items),
	})
}

func (h *DiscoveryHandler) handleClearSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	if err := h.clearHistorySvc.Execute(r.Context(), userId); err != nil {
		slog.ErrorContext(r.Context(), "clear search history failed", "error", err)
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

	var clientOccurredAt time.Time
	if req.ClientOccurredAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, req.ClientOccurredAt); parseErr == nil {
			clientOccurredAt = t
		}
	}

	input := service.RecordEventInput{
		Type:             eventType,
		QueryNorm:        req.QueryNorm,
		SearchId:         req.SearchID,
		EventId:          req.EventID,
		ClientOccurredAt: clientOccurredAt,
		Payload:          req.Payload,
	}
	if err := h.eventSvc.Execute(r.Context(), userId, input); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

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
