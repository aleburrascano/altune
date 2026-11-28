package handler

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (h *DiscoveryHandler) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q, ok := textParam(w, r, "q")
	if !ok {
		return
	}
	if q == "" {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryQueryRequired, "q parameter is required"))
		return
	}

	limit, ok := parseLimit(w, r, "limit", suggestDefaultLimit, suggestMaxLimit)
	if !ok {
		return
	}

	entries, err := h.suggestSvc.Execute(r.Context(), q, limit)
	if err != nil {
		handleError(w, r, err)
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
	httpserver.WriteJSON(w, http.StatusOK, SuggestResponse{Suggestions: dtos})
}

func (h *DiscoveryHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryQueryRequired, "q parameter is required"))
		return
	}

	limit, ok := limitOrDefault(w, r, "limit", searchDefaultLimit)
	if !ok {
		return
	}

	offset, ok := parseIntParam(w, r, "offset", 0)
	if !ok {
		return
	}

	kinds, err := parseKinds(r.URL.Query().Get("kinds"))
	if err != nil {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryInvalidKind, err.Error()))
		return
	}

	execute := h.searchSvc.ExecutePageRecorded
	if r.URL.Query().Get("save_history") == "false" {
		execute = h.searchSvc.ExecutePage
	}

	continues, ok := parseContinuedSearchId(w, r)
	if !ok {
		return
	}

	query, err := domain.NewPagedSearchQuery(q, kinds, limit, offset)
	if err != nil {
		httpserver.HandleServiceError(w, r, invalidParam(err.Error()))
		return
	}

	result, err := execute(r.Context(), userId, query, continues)
	if err != nil {
		handleError(w, r, err)
		return
	}

	results := searchResultsToDTOs(result.Results)
	topResult, sections := blendedSlateToDTOs(result.Slate)
	h.stampOwnership(r.Context(), userId, ownershipTargets(results, topResult, sections))

	if !anyProviderAnswered(result.ProviderStatuses) {
		httpserver.HandleServiceError(w, r, errAllProvidersFailed)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, DiscoverySearchResponse{
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
	h.warmUnreleased(r.Context(), result.Results)
}

const (
	warmArtistLimit = 3

	suggestDefaultLimit = 5
	suggestMaxLimit     = 10
	searchDefaultLimit  = 20
	historyDefaultLimit = 10
	historyMaxLimit     = 100
)

func (h *DiscoveryHandler) warmUnreleased(ctx context.Context, results []domain.SearchResult) {
	var artists []service.ArtistToRefresh
	for _, res := range results {
		if res.Kind != domain.ResultKindArtist || len(res.Sources) == 0 {
			continue
		}
		known := make(map[domain.ProviderName]string, len(res.Sources))
		for _, src := range res.Sources {
			known[src.Provider] = src.ExternalID
		}
		first := res.Sources[0]
		artists = append(artists, service.ArtistToRefresh{Provider: first.Provider, ExternalID: first.ExternalID, Name: res.Title, Known: known})
		if len(artists) == warmArtistLimit {
			break
		}
	}
	h.unreleasedSvc.Warm(ctx, artists)
}

func (h *DiscoveryHandler) handleSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	limit, ok := parseLimit(w, r, "limit", historyDefaultLimit, historyMaxLimit)
	if !ok {
		return
	}

	entries, err := h.historySvc.Execute(r.Context(), userId, limit)
	if err != nil {
		handleError(w, r, err)
		return
	}

	items := httpserver.MapSlice(entries, func(e *domain.SearchHistoryEntry) SearchHistoryItemDTO {
		return SearchHistoryItemDTO{
			Query:      e.Query,
			QueryNorm:  e.QueryNorm,
			ExecutedAt: e.ExecutedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		}
	})

	httpserver.WriteJSON(w, http.StatusOK, httpserver.NewList(items))
}

type RecordSearchHistoryRequest struct {
	Query string `json:"query"`
}

func (h *DiscoveryHandler) handleRecordSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var req RecordSearchHistoryRequest
	if !httpserver.DecodeJSONWithCode(w, r, &req, apierror.CodeDiscoveryInvalidBody) {
		return
	}
	if err := h.recordHistory.RecordSubmitted(r.Context(), userId, req.Query); err != nil {
		httpserver.HandleServiceError(w, r, invalidParam(err.Error()))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *DiscoveryHandler) handleClearSearchHistory(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	if err := h.clearHistorySvc.Execute(r.Context(), userId); err != nil {
		slog.WarnContext(r.Context(), "clear search history failed",
			"action", service.ClearSearchHistoryAction)
		httpserver.HandleServiceError(w, r, fmt.Errorf("%s: %w", service.ClearSearchHistoryAction, err))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *DiscoveryHandler) handleRecordEvent(w http.ResponseWriter, r *http.Request) {
	userId := requestuser.UserID(r)

	var req DiscoveryEventRequest
	if !httpserver.DecodeJSONWithCode(w, r, &req, apierror.CodeDiscoveryInvalidBody) {
		return
	}

	eventType := domain.ParseInteractionType(req.Type)
	if eventType == domain.InteractionTypeUnknown {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryInvalidEventType, "invalid event type"))
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
		CorrelationId:    boundedCorrelationID(req.CorrelationID),
		ClientOccurredAt: clientOccurredAt,
		Payload:          req.Payload,
	}
	if err := h.eventSvc.Execute(r.Context(), userId, input); err != nil {
		handleError(w, r, err)
		return
	}

	h.emitClientEvent(r, eventType, userId, req)
	w.WriteHeader(http.StatusNoContent)
}

const maxCorrelationIDLength = 64

func boundedCorrelationID(raw string) string {
	if len(raw) > maxCorrelationIDLength {
		return ""
	}
	return raw
}

func (h *DiscoveryHandler) emitClientEvent(r *http.Request, eventType domain.InteractionType, userId ids.UserId, req DiscoveryEventRequest) {
	h.clientEvents.Emit(r.Context(), telemetry.ClientEvent{
		Type:          eventType.String(),
		EventID:       req.EventID,
		CorrelationID: boundedCorrelationID(req.CorrelationID),
		AppVersion:    r.Header.Get(telemetry.AppVersionHeader),
		Platform:      r.Header.Get(telemetry.PlatformHeader),
		UserID:        userId.String(),
		Payload:       req.Payload,
	})
}

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

func parseContinuedSearchId(w http.ResponseWriter, r *http.Request) (ids.EntityId, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("search_id"))
	if raw == "" {
		return ids.EntityId{}, true
	}
	id, err := ids.ParseEntityId(raw)
	if err != nil {
		httpserver.HandleServiceError(w, r, invalidParam("search_id must be a uuid"))
		return ids.EntityId{}, false
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
