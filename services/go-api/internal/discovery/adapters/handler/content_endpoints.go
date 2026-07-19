package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

// Content-fetch endpoints: album tracks, artist top-tracks/albums, related
// tracks — the detail-screen surfaces reached by (provider, externalId).

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
		Provider: resp.ProviderName.String(),
		Status:   resp.Status.String(),
		Items:    items,
	}
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

// clampLimit parses the "limit" query param, falling back to def when absent
// or non-positive and capping it at max.
func clampLimit(r *http.Request, def, max int) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}

// writeContentFetchError writes the degraded envelope every content-fetch
// handler falls back to when its service isn't wired.
func writeContentFetchError(w http.ResponseWriter, provider string) {
	httputil.WriteJSON(w, http.StatusOK, ContentFetchResponseDTO{
		Provider: provider, Status: "error", Items: []SearchResultDTO{},
	})
}

func (h *DiscoveryHandler) handleAlbumTracks(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	limit := clampLimit(r, 50, 100)
	albumTitle := strings.TrimSpace(r.URL.Query().Get("title"))
	albumArtist := strings.TrimSpace(r.URL.Query().Get("artist"))

	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httputil.BadRequest(w, "unknown provider")
		return
	}

	if h.albumSvc == nil {
		writeContentFetchError(w, provider)
		return
	}

	resp, err := h.albumSvc.Execute(r.Context(), pn, externalID, albumTitle, albumArtist, limit)
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
	limit := clampLimit(r, 5, 50)

	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httputil.BadRequest(w, "unknown provider")
		return
	}

	if h.artistSvc == nil {
		writeContentFetchError(w, provider)
		return
	}

	resp, err := h.artistSvc.GetTopTracks(r.Context(), pn, externalID, limit)
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
	limit := clampLimit(r, 50, 100)
	artistName := strings.TrimSpace(r.URL.Query().Get("name"))

	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httputil.BadRequest(w, "unknown provider")
		return
	}

	if h.artistSvc == nil {
		writeContentFetchError(w, provider)
		return
	}

	resp, err := h.artistSvc.GetAlbums(r.Context(), pn, externalID, artistName, limit)
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
	limit := clampLimit(r, 20, 50)

	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httputil.BadRequest(w, "unknown provider")
		return
	}

	if h.relatedSvc == nil {
		writeContentFetchError(w, provider)
		return
	}

	resp, err := h.relatedSvc.Execute(r.Context(), pn, externalID, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "get related tracks failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, contentFetchToDTO(resp))
}
