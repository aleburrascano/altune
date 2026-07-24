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

// Search-family endpoints: /search, /suggest, /search-history, /events — the
// surfaces that change with the ranking pipeline — plus the search wire DTOs
// shared by every endpoint family (SearchResultDTO and its mappers).

// --- DTOs ---

type SearchResultDTO struct {
	Kind            string         `json:"kind"`
	Title           string         `json:"title"`
	Subtitle        string         `json:"subtitle,omitempty"`
	ImageURL        string         `json:"image_url,omitempty"`
	ArtworkSource   string         `json:"artwork_source,omitempty"`
	Confidence      string         `json:"confidence"`
	ResultSignature string         `json:"result_signature"`
	Sources         []SourceRefDTO `json:"sources"`
	Extras          map[string]any `json:"extras"`
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
	SearchID       string              `json:"search_id"`
	Results        []SearchResultDTO   `json:"results"`
	Providers      []ProviderStatusDTO `json:"providers"`
	Partial        bool                `json:"partial"`
	Exploration    bool                `json:"exploration,omitempty"`
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
	Type             string         `json:"type"`
	QueryNorm        string         `json:"query_norm"`
	SearchID         string         `json:"search_id"`
	EventID          string         `json:"event_id"`
	ClientOccurredAt string         `json:"client_occurred_at"`
	Payload          map[string]any `json:"payload"`
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

	query, err := domain.NewSearchQuery(q, kinds, limit)
	if err != nil {
		httputil.BadRequest(w, err.Error())
		return
	}

	result, err := h.searchSvc.Execute(r.Context(), userId, query, saveHistory)
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
		SearchID:       result.SearchId,
		Results:        searchResultsToDTOs(result.Results),
		Providers:      providerStatusesToDTOs(result.ProviderStatuses),
		Partial:        result.Partial,
		Exploration:    result.Explored,
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
			Query:     e.Query,
			QueryNorm: e.QueryNorm,
			// .UTC() first: the layout hard-codes the Z designator, which would lie
			// about a non-UTC time.
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

	// client_occurred_at is optional RFC3339; a malformed value is dropped (the
	// server received_at still anchors the event in time).
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
	// HandleServiceError renders service validation errors (StatusError → 400:
	// non-client-submittable type, wrong payload value type) and 500s the rest.
	if err := h.eventSvc.Execute(r.Context(), userId, input); err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- wire mapping ---

func searchResultToDTO(sr domain.SearchResult) SearchResultDTO {
	sources := make([]SourceRefDTO, len(sr.Sources))
	for i, s := range sr.Sources {
		sources[i] = SourceRefDTO{
			Provider:   s.Provider.String(),
			ExternalID: s.ExternalID,
			URL:        s.URL,
		}
	}
	// The wire extras keep carrying the metadata the domain promoted to typed
	// fields — clients key on these names, so the contract stays byte-identical.
	extras := make(map[string]any, len(sr.Extras)+7)
	for k, v := range sr.Extras {
		extras[k] = v
	}
	if sr.ISRC != "" {
		extras["isrc"] = sr.ISRC
	}
	if sr.UPC != "" {
		extras["upc"] = sr.UPC
	}
	if sr.MBID != "" {
		extras["mbid"] = sr.MBID
	}
	if sr.Year != 0 {
		extras["year"] = sr.Year
	}
	if sr.ReleaseDate != "" {
		extras["release_date"] = sr.ReleaseDate
	}
	if sr.TrackCount != 0 {
		extras["track_count"] = sr.TrackCount
	}
	if sr.ProviderRank != 0 {
		extras["rank"] = sr.ProviderRank
	}
	if sr.FanCount != 0 {
		extras["nb_fan"] = sr.FanCount
	}
	// Prefer the signature stamped at rank time (pre-disambiguation): recomputing
	// here after enrichment filled artist subtitles would drift from the key the
	// behavioral score map uses (see domain.SearchResult.Signature). Compute only
	// as a fallback for paths that never went through mergeRankEnrich.
	signature := sr.Signature
	if signature == "" {
		signature = domain.ResultSignature(sr)
	}
	return SearchResultDTO{
		Kind:            sr.Kind.String(),
		Title:           sr.Title,
		Subtitle:        sr.Subtitle,
		ImageURL:        sr.ImageURL,
		ArtworkSource:   sr.ArtworkSource,
		Confidence:      sr.Confidence.String(),
		ResultSignature: signature,
		Sources:         sources,
		Extras:          extras,
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
