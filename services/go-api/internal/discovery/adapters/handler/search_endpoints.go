package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/core/history"
	"altune/go-api/internal/discovery/service/interaction"
	"altune/go-api/internal/discovery/service/unreleased"
	shareddomain "altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/httputil"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/telemetry"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (h *DiscoveryHandler) handleSuggest(w http.ResponseWriter, r *http.Request) {
	_, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	q, ok := textParam(w, r, "q")
	if !ok {
		return
	}
	if q == "" {
		httputil.HandleServiceError(w, r, shareddomain.NewError(shareddomain.CodeDiscoveryQueryRequired, "q parameter is required"))
		return
	}

	limit, ok := parseLimit(w, r, "limit", 5, 10, resetToDefault)
	if !ok {
		return
	}

	entries, err := h.suggestSvc.Execute(r.Context(), q, limit)
	if err != nil {
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
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		httputil.HandleServiceError(w, r, shareddomain.NewError(shareddomain.CodeDiscoveryQueryRequired, "q parameter is required"))
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
		httputil.HandleServiceError(w, r, shareddomain.NewError(shareddomain.CodeDiscoveryInvalidKind, err.Error()))
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
		httputil.HandleServiceError(w, r, invalidParam(err.Error()))
		return
	}

	result, err := h.searchSvc.ExecutePage(r.Context(), userId, query, saveHistory, continues)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	results := searchResultsToDTOs(result.Results)
	topResult, sections := blendedSlateToDTOs(result.Slate)
	h.ownership.StampOwnership(r.Context(), userId, ownershipTargets(results, topResult, sections))

	if !anyProviderAnswered(result.ProviderStatuses) {
		httputil.HandleServiceError(w, r, errAllProvidersFailed)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, DiscoverySearchResponse{
		Query:          q,
		QueryNorm:      result.QueryNorm,
		SearchID:       result.SearchId,
		Results:        results,
		TopResult:      topResult,
		Sections:       sections,
		Providers:      providerStatusesToDTOs(result.ProviderStatuses),
		Partial:        result.Partial,
		Exploration:    result.Explored,
		Cache:          CacheDTO{Hit: result.Cached, FetchedAt: nil},
		CorrectedQuery: result.CorrectedQuery,
		OriginalQuery:  result.OriginalQuery,
		Related:        relatedGroupsToDTOs(result.Related),
		Total:          result.Total,
		Offset:         result.Offset,
		HasMore:        result.HasMore,
	})
	h.warmUnreleased(result.Results)
}

const warmArtistLimit = 3

func (h *DiscoveryHandler) warmUnreleased(results []domain.SearchResult) {
	if h.unreleasedSvc == nil {
		return
	}
	var artists []unreleased.WarmArtist
	for _, res := range results {
		if res.Kind != domain.ResultKindArtist || len(res.Sources) == 0 {
			continue
		}
		known := make(map[domain.ProviderName]string, len(res.Sources))
		for _, src := range res.Sources {
			known[src.Provider] = src.ExternalID
		}
		first := res.Sources[0]
		artists = append(artists, unreleased.WarmArtist{Provider: first.Provider, ExternalID: first.ExternalID, Name: res.Title, Known: known})
		if len(artists) == warmArtistLimit {
			break
		}
	}
	h.unreleasedSvc.Warm(artists)
}

func (h *DiscoveryHandler) handleSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	limit, ok := parseLimit(w, r, "limit", 10, 100, clampToMax)
	if !ok {
		return
	}

	entries, err := h.historySvc.Execute(r.Context(), userId, limit)
	if err != nil {
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

type RecordSearchHistoryRequest struct {
	Query string `json:"query"`
}

func (h *DiscoveryHandler) handleRecordSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	var req RecordSearchHistoryRequest
	if !httputil.DecodeJSONWithCode(w, r, &req, shareddomain.CodeDiscoveryInvalidBody) {
		return
	}
	if err := h.recordHistory.RecordSubmitted(r.Context(), userId, req.Query); err != nil {
		httputil.HandleServiceError(w, r, invalidParam(err.Error()))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *DiscoveryHandler) handleClearSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	if err := h.clearHistorySvc.Execute(r.Context(), userId); err != nil {
		slog.WarnContext(r.Context(), "clear search history failed",
			"action", history.ClearSearchHistoryAction,
			"user_id", userId.String())
		httputil.HandleServiceError(w, r, fmt.Errorf("%s: %w", history.ClearSearchHistoryAction, err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *DiscoveryHandler) handleRecordEvent(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	var req DiscoveryEventRequest
	if !httputil.DecodeJSONWithCode(w, r, &req, shareddomain.CodeDiscoveryInvalidBody) {
		return
	}

	eventType := domain.ParseEventType(req.Type)
	if eventType == domain.EventTypeUnknown {
		httputil.HandleServiceError(w, r, shareddomain.NewError(shareddomain.CodeDiscoveryInvalidEventType, "invalid event type"))
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

	input := interaction.RecordEventInput{
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

	h.emitClientEvent(r, eventType, userId, req)
	w.WriteHeader(http.StatusNoContent)
}

func (h *DiscoveryHandler) emitClientEvent(r *http.Request, eventType domain.EventType, userId shared.UserId, req DiscoveryEventRequest) {
	if h.clientEvents == nil {
		return
	}
	h.clientEvents.Emit(r.Context(), telemetry.ClientEvent{
		Type:       eventType.String(),
		EventID:    req.EventID,
		AppVersion: r.Header.Get(telemetry.AppVersionHeader),
		Platform:   r.Header.Get(telemetry.PlatformHeader),
		UserID:     userId.String(),
		Payload:    req.Payload,
	})
}

var errAllProvidersFailed = shareddomain.NewError(shareddomain.CodeDiscoveryAllProvidersFailed, "")

func anyProviderAnswered(statuses []domain.ProviderSearchResponse) bool {
	if len(statuses) == 0 {
		return true
	}
	for _, ps := range statuses {
		if ps.Status == domain.ProviderStatusOK {
			return true
		}
	}
	return false
}

func parseContinuedSearchId(w http.ResponseWriter, r *http.Request) (shared.EntityId, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("search_id"))
	if raw == "" {
		return shared.EntityId{}, true
	}
	id, err := shared.ParseEntityId(raw)
	if err != nil {
		httputil.HandleServiceError(w, r, invalidParam("search_id must be a uuid"))
		return shared.EntityId{}, false
	}
	return id, true
}

func parseKinds(csv string) (domain.ResultKindSet, error) {
	if csv == "" {
		return domain.AllKinds(), nil
	}
	kinds, invalid := parseKindTokens(csv)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid kinds: %s", strings.Join(invalid, ", "))
	}
	if len(kinds) == 0 {
		return domain.AllKinds(), nil
	}
	return kinds, nil
}

func parseKindTokens(csv string) (domain.ResultKindSet, []string) {
	kinds := make(domain.ResultKindSet)
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
	return kinds, invalid
}
