package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

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

	resp, err := h.albumSvc.ExecuteRequest(r.Context(), service.AlbumTracksRequest{
		Provider:     pn,
		ExternalID:   externalID,
		Title:        albumTitle,
		Artist:       albumArtist,
		MBExternalID: strings.TrimSpace(r.URL.Query().Get("mbid")),
		Limit:        limit,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "get album tracks failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	dto := contentFetchToDTO(resp)
	if userId, hasUser := auth.UserIDFromContext(r.Context()); hasUser {
		h.stampOwnership(r.Context(), userId, dto.Items)
		h.fillAlbumTrackNumbers(r.Context(), userId, dto.Items)
	}
	httputil.WriteJSON(w, http.StatusOK, dto)
}

func (h *DiscoveryHandler) handleArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	limit := clampLimit(r, 5, 50)
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

	resp, err := h.artistSvc.GetTopTracks(r.Context(), pn, externalID, artistName, limit)
	if err != nil {
		slog.ErrorContext(r.Context(), "get artist top tracks failed",
			"error", err, "provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	if h.searchTrace != nil {
		h.searchTrace.RecordContentFetch(r.Context(), "top_tracks", provider, "", resp.Status.String(), resp.Items)
	}

	h.writeContentFetch(w, r, resp)
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

	h.writeContentFetch(w, r, resp)
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

	h.writeContentFetch(w, r, resp)
}

type ArtistContentResponseDTO struct {
	TopTracks ContentFetchResponseDTO `json:"top_tracks"`
	Albums    ContentFetchResponseDTO `json:"albums"`
}

func (h *DiscoveryHandler) handleArtistContent(w http.ResponseWriter, r *http.Request) {
	provider, externalID, ok := validateContentParams(w, r)
	if !ok {
		return
	}
	artistName := strings.TrimSpace(r.URL.Query().Get("name"))
	tracksLimit := clampNamedLimit(r, "tracks_limit", 5, 50)
	albumsLimit := clampNamedLimit(r, "albums_limit", 100, 200)

	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httputil.BadRequest(w, "unknown provider")
		return
	}
	if h.artistSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, ArtistContentResponseDTO{
			TopTracks: ContentFetchResponseDTO{Provider: provider, Status: "error", Items: []SearchResultDTO{}},
			Albums:    ContentFetchResponseDTO{Provider: provider, Status: "error", Items: []SearchResultDTO{}},
		})
		return
	}

	var tracksResp, albumsResp *service.ContentFetchResponse
	var tracksErr, albumsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tracksResp, tracksErr = h.artistSvc.GetTopTracks(r.Context(), pn, externalID, artistName, tracksLimit)
	}()
	go func() {
		defer wg.Done()
		albumsResp, albumsErr = h.artistSvc.GetAlbums(r.Context(), pn, externalID, artistName, albumsLimit)
	}()
	wg.Wait()

	if tracksErr != nil || albumsErr != nil {
		slog.ErrorContext(r.Context(), "artist content failed",
			"tracks_error", tracksErr, "albums_error", albumsErr,
			"provider", provider, "external_id", externalID)
		httputil.InternalError(w)
		return
	}

	if h.searchTrace != nil {
		h.searchTrace.RecordContentFetch(r.Context(), "top_tracks", provider, artistName, tracksResp.Status.String(), tracksResp.Items)
		h.searchTrace.RecordContentFetch(r.Context(), "albums", provider, artistName, albumsResp.Status.String(), albumsResp.Items)
	}

	dto := ArtistContentResponseDTO{
		TopTracks: contentFetchToDTO(tracksResp),
		Albums:    contentFetchToDTO(albumsResp),
	}
	if userId, hasUser := auth.UserIDFromContext(r.Context()); hasUser {
		h.stampOwnership(r.Context(), userId, dto.TopTracks.Items)
	}
	httputil.WriteJSON(w, http.StatusOK, dto)
}

func clampNamedLimit(r *http.Request, param string, def, max int) int {
	limit, _ := strconv.Atoi(r.URL.Query().Get(param))
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
