package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Error codes a rejected discovery request answers with, one per cause, so a
// client branches on the code instead of matching the detail text.
const (
	requestCodeQRequired         = "discovery.q_required"
	requestCodeInvalidKind       = "discovery.invalid_kind"
	requestCodeInvalidProvider   = "discovery.invalid_provider"
	requestCodeInvalidParam      = "discovery.invalid_param"
	requestCodeInvalidEventType  = "discovery.invalid_event_type"
	requestCodeInvalidBody       = "discovery.invalid_body"
	searchCodeAllProvidersFailed = "discovery.all_providers_failed"
)

func (h *DiscoveryHandler) handleSuggest(w http.ResponseWriter, r *http.Request) {
	_, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httputil.BadRequestCode(w, requestCodeQRequired, "q parameter is required")
		return
	}

	limit, ok := parseLimit(w, r, "limit", 5, 10, resetToDefault)
	if !ok {
		return
	}

	entries, err := h.suggestSvc.Execute(r.Context(), q, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "suggest failed", "error", err)
		httputil.HandleServiceError(w, r, err)
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
	if strings.TrimSpace(q) == "" {
		httputil.BadRequestCode(w, requestCodeQRequired, "q parameter is required")
		return
	}

	limit, ok := limitOrDefault(w, r, "limit", 20)
	if !ok {
		return
	}

	offset, ok := parseIntParam(w, r, "offset", 0)
	if !ok {
		return
	}

	kinds, err := parseKinds(r.URL.Query().Get("kinds"))
	if err != nil {
		httputil.BadRequestCode(w, requestCodeInvalidKind, err.Error())
		return
	}

	saveHistory := true
	if r.URL.Query().Get("save_history") == "false" {
		saveHistory = false
	}

	continues, ok := parseContinuedSearchId(w, r)
	if !ok {
		return
	}

	query, err := domain.NewPagedSearchQuery(q, kinds, limit, offset)
	if err != nil {
		httputil.BadRequestCode(w, requestCodeInvalidParam, err.Error())
		return
	}

	result, err := h.searchSvc.ExecutePage(r.Context(), userId, query, saveHistory, continues)
	if err != nil {
		slog.ErrorContext(r.Context(), "search failed", "error", err)
		httputil.HandleServiceError(w, r, err)
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
	h.ownership.StampOwnership(r.Context(), userId, ownershipTargets(results, topResult, sections))

	status, code := searchOutcome(result.ProviderStatuses)
	httputil.WriteJSON(w, status, DiscoverySearchResponse{
		Code:           code,
		Query:          q,
		QueryNorm:      result.QueryNorm,
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

	limit, ok := parseLimit(w, r, "limit", 10, 100, clampToMax)
	if !ok {
		return
	}

	entries, err := h.historySvc.Execute(r.Context(), userId, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "search history failed", "error", err)
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := httputil.MapSlice(entries, func(e *domain.SearchHistoryEntry) SearchHistoryItemDTO {
		return SearchHistoryItemDTO{
			Query:      e.Query,
			QueryNorm:  e.QueryNorm,
			ExecutedAt: e.ExecutedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		}
	})

	httputil.WriteJSON(w, http.StatusOK, httputil.NewList(items))
}

func (h *DiscoveryHandler) handleClearSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	if err := h.clearHistorySvc.Execute(r.Context(), userId); err != nil {
		slog.ErrorContext(r.Context(), "clear search history failed",
			"action", service.ClearSearchHistoryAction,
			"user_id", userId.String(),
			"error", err)
		httputil.HandleServiceError(w, r, err)
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
		httputil.BadRequestCode(w, requestCodeInvalidBody, "invalid request body")
		return
	}

	eventType := domain.ParseEventType(req.Type)
	if eventType == domain.EventTypeUnknown {
		httputil.BadRequestCode(w, requestCodeInvalidEventType, "invalid event type")
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

// searchOutcome maps a scatter's provider statuses onto the response's HTTP
// status and error code. One provider answering is enough for a 200, so the
// 503 and its code mean every provider in the fan-out failed.
func searchOutcome(statuses []domain.ProviderSearchResponse) (int, string) {
	if len(statuses) == 0 {
		return http.StatusOK, ""
	}
	for _, ps := range statuses {
		if ps.Status == domain.ProviderStatusOK {
			return http.StatusOK, ""
		}
	}
	return http.StatusServiceUnavailable, searchCodeAllProvidersFailed
}

// parseContinuedSearchId reads the search_id a caller echoes back from the page
// it already has, which keeps later pages cut from that search's ranking. No
// search_id means a new search, so old clients page exactly as before.
func parseContinuedSearchId(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("search_id"))
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		httputil.BadRequestCode(w, requestCodeInvalidParam, "search_id must be a uuid")
		return uuid.Nil, false
	}
	return id, true
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
