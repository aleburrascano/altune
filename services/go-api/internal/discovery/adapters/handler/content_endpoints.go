package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

type ContentFetchResponseDTO struct {
	Provider string            `json:"provider_name"`
	Status   string            `json:"status"`
	Items    []SearchResultDTO `json:"items"`
	// Partial is true when a provider in the fan-out failed while others
	// answered, so Items may be incomplete. Mirrors search's partial flag.
	Partial bool `json:"partial"`
	// Code names why the fetch failed, and is absent when it did not. Unlike
	// Status, it separates a provider with no adapter for this content kind
	// from one that was called and failed.
	Code string `json:"code,omitempty"`
}

func contentFetchToDTO(resp *service.ContentFetchResponse) ContentFetchResponseDTO {
	items := make([]SearchResultDTO, len(resp.Items))
	for i, r := range resp.Items {
		items[i] = searchResultToDTO(r)
	}
	_, code := contentFetchOutcome(resp)
	return ContentFetchResponseDTO{
		Provider: resp.ProviderName.String(),
		Status:   resp.Status.String(),
		Items:    items,
		Partial:  resp.Partial,
		Code:     code,
	}
}

// Error codes a content fetch that fully failed answers with, one per cause.
const (
	contentCodeUnserved        = "discovery.content_unserved"
	contentCodeProviderTimeout = "discovery.provider_timeout"
	contentCodeRateLimited     = "discovery.provider_rate_limited"
	contentCodeCircuitOpen     = "discovery.provider_circuit_open"
	contentCodeProviderError   = "discovery.provider_error"
)

// contentFetchOutcome maps a content fetch onto its HTTP status and error
// code. A fetch with an ok status answers 200 with no code, even when Partial.
// A failed one answers non-2xx so monitoring sees it: 404 when no provider is
// wired for the content (permanent), 504 for an upstream timeout (retry now),
// 503 for a throttled upstream or an open circuit (retry later), and 502 for
// any other upstream failure.
func contentFetchOutcome(resp *service.ContentFetchResponse) (int, string) {
	if resp.Unserved {
		return http.StatusNotFound, contentCodeUnserved
	}
	switch resp.Status {
	case domain.ProviderStatusOK:
		return http.StatusOK, ""
	case domain.ProviderStatusTimeout:
		return http.StatusGatewayTimeout, contentCodeProviderTimeout
	case domain.ProviderStatusRateLimited:
		return http.StatusServiceUnavailable, contentCodeRateLimited
	case domain.ProviderStatusCircuitOpen:
		return http.StatusServiceUnavailable, contentCodeCircuitOpen
	default:
		return http.StatusBadGateway, contentCodeProviderError
	}
}

// unservedContentDTO is the answer for a content kind no service is wired for.
func unservedContentDTO(provider string) ContentFetchResponseDTO {
	return ContentFetchResponseDTO{
		Provider: provider, Status: domain.ProviderStatusError.String(), Items: []SearchResultDTO{},
		Code: contentCodeUnserved,
	}
}

func writeContentFetchError(w http.ResponseWriter, provider string) {
	httputil.WriteJSON(w, http.StatusNotFound, unservedContentDTO(provider))
}

func degradeUnserved(w http.ResponseWriter) func(provider string) {
	return func(provider string) { writeContentFetchError(w, provider) }
}

func failContentFetch(w http.ResponseWriter, r *http.Request, err error, msg, provider, externalID string) {
	slog.ErrorContext(r.Context(), msg,
		"error", err, "provider", provider, "external_id", externalID)
	httputil.HandleServiceError(w, r, err)
}

func withProvider(
	w http.ResponseWriter,
	r *http.Request,
	available bool,
	degrade func(provider string),
	call func(pn domain.ProviderName, provider, externalID string),
) {
	provider := chi.URLParam(r, "provider")
	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httputil.BadRequestCode(w, requestCodeInvalidProvider, "unknown provider")
		return
	}
	externalID, ok := externalIDParam(w, r, pn)
	if !ok {
		return
	}
	if !available {
		degrade(provider)
		return
	}
	call(pn, provider, externalID)
}

func (h *DiscoveryHandler) handleAlbumTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.albumSvc != nil,
		degradeUnserved(w),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 50, 100, clampToMax)
			if !ok {
				return
			}
			albumTitle, ok := textParam(w, r, "title")
			if !ok {
				return
			}
			albumArtist, ok := textParam(w, r, "artist")
			if !ok {
				return
			}
			albumMBID, ok := mbidParam(w, r)
			if !ok {
				return
			}

			resp, err := h.albumSvc.ExecuteRequest(r.Context(), service.AlbumTracksRequest{
				Provider:     pn,
				ExternalID:   externalID,
				Title:        albumTitle,
				Artist:       albumArtist,
				MBExternalID: albumMBID,
				Limit:        limit,
			})
			if err != nil {
				failContentFetch(w, r, err, "get album tracks failed", provider, externalID)
				return
			}

			dto := contentFetchToDTO(resp)
			if userId, hasUser := auth.UserIDFromContext(r.Context()); hasUser {
				h.ownership.EnrichAlbumTracks(r.Context(), userId, ownableItems(dto.Items))
			}
			status, _ := contentFetchOutcome(resp)
			httputil.WriteJSON(w, status, dto)
		})
}

func (h *DiscoveryHandler) handleArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.artistSvc != nil,
		degradeUnserved(w),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 5, 50, clampToMax)
			if !ok {
				return
			}
			artistName, ok := textParam(w, r, "name")
			if !ok {
				return
			}

			resp, err := h.artistSvc.GetTopTracks(r.Context(), pn, externalID, artistName, limit)
			if err != nil {
				failContentFetch(w, r, err, "get artist top tracks failed", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

func (h *DiscoveryHandler) handleArtistAlbums(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.artistSvc != nil,
		degradeUnserved(w),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 50, 100, clampToMax)
			if !ok {
				return
			}
			artistName, ok := textParam(w, r, "name")
			if !ok {
				return
			}

			resp, err := h.artistSvc.GetAlbums(r.Context(), pn, externalID, artistName, limit)
			if err != nil {
				failContentFetch(w, r, err, "get artist albums failed", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

func (h *DiscoveryHandler) handleRelatedTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.relatedSvc != nil,
		degradeUnserved(w),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 20, 50, clampToMax)
			if !ok {
				return
			}

			resp, err := h.relatedSvc.Execute(r.Context(), pn, externalID, limit)
			if err != nil {
				failContentFetch(w, r, err, "get related tracks failed", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

type ArtistContentResponseDTO struct {
	// Code is set only when both halves failed, and is then the code the
	// response's HTTP status was taken from.
	Code      string                  `json:"code,omitempty"`
	TopTracks ContentFetchResponseDTO `json:"top_tracks"`
	Albums    ContentFetchResponseDTO `json:"albums"`
}

// artistContentOutcome is the HTTP status and top-level code for the combined
// artist content response. It fails only when both halves failed, so a
// response with either half's content stays 200. When the halves failed for
// different reasons, a provider that was called and failed outranks one with
// no adapter, and top tracks break any remaining tie.
func artistContentOutcome(tracks, albums *service.ContentFetchResponse) (int, string) {
	tracksStatus, tracksCode := contentFetchOutcome(tracks)
	albumsStatus, albumsCode := contentFetchOutcome(albums)
	if tracksStatus == http.StatusOK || albumsStatus == http.StatusOK {
		return http.StatusOK, ""
	}
	if tracks.Unserved && !albums.Unserved {
		return albumsStatus, albumsCode
	}
	return tracksStatus, tracksCode
}

// runRecovered runs one of handleArtistContent's fetches inside its goroutine,
// containing a panic by logging it and returning it as that fetch's error, so
// the request fails on its own instead of the panic terminating the process.
func runRecovered(ctx context.Context, event string, fetch func() error) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, event, "panic", rec)
			err = fmt.Errorf("recovered panic: %v", rec)
		}
	}()
	return fetch()
}

func (h *DiscoveryHandler) handleArtistContent(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.artistSvc != nil,
		func(provider string) {
			httputil.WriteJSON(w, http.StatusNotFound, ArtistContentResponseDTO{
				Code:      contentCodeUnserved,
				TopTracks: unservedContentDTO(provider),
				Albums:    unservedContentDTO(provider),
			})
		},
		func(pn domain.ProviderName, provider, externalID string) {
			artistName, ok := textParam(w, r, "name")
			if !ok {
				return
			}
			tracksLimit, ok := parseLimit(w, r, "tracks_limit", 5, 50, clampToMax)
			if !ok {
				return
			}
			albumsLimit, ok := parseLimit(w, r, "albums_limit", 100, 200, clampToMax)
			if !ok {
				return
			}

			var tracksResp, albumsResp *service.ContentFetchResponse
			var tracksErr, albumsErr error
			// Both fetches start together, so the shared start clocks each one.
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				tracksErr = runRecovered(r.Context(), "artist_content.top_tracks_panic", func() error {
					var err error
					tracksResp, err = h.artistSvc.GetTopTracks(r.Context(), pn, externalID, artistName, tracksLimit)
					return err
				})
			}()
			go func() {
				defer wg.Done()
				albumsErr = runRecovered(r.Context(), "artist_content.albums_panic", func() error {
					var err error
					albumsResp, err = h.artistSvc.GetAlbums(r.Context(), pn, externalID, artistName, albumsLimit)
					return err
				})
			}()
			wg.Wait()

			if tracksErr != nil || albumsErr != nil {
				slog.ErrorContext(r.Context(), "artist content failed",
					"tracks_error", tracksErr, "albums_error", albumsErr,
					"provider", provider, "external_id", externalID)
				httputil.HandleServiceError(w, r, errors.Join(tracksErr, albumsErr))
				return
			}

			status, code := artistContentOutcome(tracksResp, albumsResp)
			dto := ArtistContentResponseDTO{
				Code:      code,
				TopTracks: contentFetchToDTO(tracksResp),
				Albums:    contentFetchToDTO(albumsResp),
			}
			if userId, hasUser := auth.UserIDFromContext(r.Context()); hasUser {
				h.ownership.StampOwnership(r.Context(), userId, ownableItems(dto.TopTracks.Items))
			}
			httputil.WriteJSON(w, status, dto)
		})
}
