package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/content"
	"altune/go-api/internal/discovery/service/providercall"
	shareddomain "altune/go-api/internal/shared/domain"
	"altune/go-api/internal/shared/fanout"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type ContentFetchResponseDTO struct {
	Provider string            `json:"provider_name"`
	Status   string            `json:"status"`
	Items    []SearchResultDTO `json:"items"`
	Partial  bool              `json:"partial"`
	Code     string            `json:"code,omitempty"`
}

func contentFetchToDTO(resp *providercall.ContentFetchResponse) ContentFetchResponseDTO {
	items := make([]SearchResultDTO, len(resp.Items))
	for i, r := range resp.Items {
		items[i] = searchResultToDTO(r)
	}
	return ContentFetchResponseDTO{
		Provider: resp.ProviderName.String(),
		Status:   resp.Status.String(),
		Items:    items,
		Partial:  resp.Partial,
		Code:     codeOf(contentFetchFailure(resp)),
	}
}

var errContentUnserved = shareddomain.NewError(shareddomain.CodeDiscoveryContentUnserved, "")

func contentFetchFailure(resp *providercall.ContentFetchResponse) *shareddomain.CodedError {
	if resp.Unserved {
		return errContentUnserved
	}
	switch resp.Status {
	case domain.ProviderStatusOK:
		return nil
	case domain.ProviderStatusTimeout:
		return shareddomain.NewError(shareddomain.CodeDiscoveryProviderTimeout, "")
	case domain.ProviderStatusRateLimited:
		return shareddomain.NewError(shareddomain.CodeDiscoveryProviderRateLimited, "")
	case domain.ProviderStatusCircuitOpen:
		return shareddomain.NewError(shareddomain.CodeDiscoveryProviderCircuitOpen, "")
	default:
		return shareddomain.NewError(shareddomain.CodeDiscoveryProviderError, "")
	}
}

func codeOf(failure *shareddomain.CodedError) string {
	if failure == nil {
		return ""
	}
	return failure.Code().String()
}

func degradeUnserved(w http.ResponseWriter, r *http.Request) func(provider string) {
	return func(provider string) {
		httputil.HandleServiceError(w, r, fmt.Errorf("%s content: %w", provider, errContentUnserved))
	}
}

func failContentFetch(w http.ResponseWriter, r *http.Request, err error, operation, provider, externalID string) {
	httputil.HandleServiceError(w, r, fmt.Errorf("%s for %s %q: %w", operation, provider, externalID, err))
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
		httputil.HandleServiceError(w, r, shareddomain.NewError(shareddomain.CodeDiscoveryInvalidProvider, "unknown provider"))
		return
	}
	externalID, ok := externalIDParam(w, r)
	if !ok {
		return
	}
	if !available {
		degrade(provider)
		return
	}
	call(pn, provider, externalID)
}

const maxAlbumTracks = 250

func (h *DiscoveryHandler) handleAlbumTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.albumSvc != nil,
		degradeUnserved(w, r),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", maxAlbumTracks, maxAlbumTracks, clampToMax)
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

			resp, err := h.albumSvc.ExecuteRequest(r.Context(), content.AlbumTracksRequest{
				Provider:     pn,
				ExternalID:   externalID,
				Title:        albumTitle,
				Artist:       albumArtist,
				MBExternalID: albumMBID,
				Limit:        limit,
				Known:        knownIDsParam(r),
			})
			if err != nil {
				failContentFetch(w, r, err, "get album tracks", provider, externalID)
				return
			}

			if failure := contentFetchFailure(resp); failure != nil {
				httputil.HandleServiceError(w, r, fmt.Errorf("album tracks for %s %q: %w", provider, externalID, failure))
				return
			}
			dto := contentFetchToDTO(resp)
			if userId, hasUser := requestuser.UserIDFromContext(r.Context()); hasUser {
				h.ownership.EnrichAlbumTracks(r.Context(), userId, albumTrackItems(albumTitle, dto.Items))
			}
			httputil.WriteJSON(w, http.StatusOK, dto)
		})
}

func (h *DiscoveryHandler) handleArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.artistSvc != nil,
		degradeUnserved(w, r),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 5, 50, clampToMax)
			if !ok {
				return
			}
			known := knownIDsParam(r)

			resp, err := h.artistSvc.GetTopTracks(r.Context(), pn, externalID, known, limit)
			if err != nil {
				failContentFetch(w, r, err, "get artist top tracks", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

func (h *DiscoveryHandler) handleArtistAlbums(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.artistSvc != nil,
		degradeUnserved(w, r),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 50, 100, clampToMax)
			if !ok {
				return
			}
			known := knownIDsParam(r)

			resp, err := h.artistSvc.GetAlbums(r.Context(), pn, externalID, known, limit)
			if err != nil {
				failContentFetch(w, r, err, "get artist albums", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

func (h *DiscoveryHandler) handleRelatedTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.relatedSvc != nil,
		degradeUnserved(w, r),
		func(pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 20, 50, clampToMax)
			if !ok {
				return
			}

			resp, err := h.relatedSvc.Execute(r.Context(), pn, externalID, limit)
			if err != nil {
				failContentFetch(w, r, err, "get related tracks", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

type ArtistContentResponseDTO struct {
	TopTracks ContentFetchResponseDTO `json:"top_tracks"`
	Albums    ContentFetchResponseDTO `json:"albums"`
}

func artistContentFailure(tracks, albums *providercall.ContentFetchResponse) *shareddomain.CodedError {
	tracksFailure, albumsFailure := contentFetchFailure(tracks), contentFetchFailure(albums)
	if tracksFailure == nil || albumsFailure == nil {
		return nil
	}
	if tracks.Unserved && !albums.Unserved {
		return albumsFailure
	}
	return tracksFailure
}

func (h *DiscoveryHandler) fetchTopTracksAndAlbums(
	ctx context.Context, pn domain.ProviderName, externalID string, known map[domain.ProviderName]string, tracksLimit, albumsLimit int,
) (tracksResp, albumsResp *providercall.ContentFetchResponse, tracksErr, albumsErr error) {
	g := fanout.New("artist_content.fetch", 2)
	g.Go(ctx, func() {
		tracksResp, tracksErr = fetchContentPart(ctx, "artist_content.top_tracks_panic", func() (*providercall.ContentFetchResponse, error) {
			return h.artistSvc.GetTopTracks(ctx, pn, externalID, known, tracksLimit)
		})
	})
	g.Go(ctx, func() {
		albumsResp, albumsErr = fetchContentPart(ctx, "artist_content.albums_panic", func() (*providercall.ContentFetchResponse, error) {
			return h.artistSvc.GetAlbums(ctx, pn, externalID, known, albumsLimit)
		})
	})
	_ = g.Wait()
	return tracksResp, albumsResp, tracksErr, albumsErr
}

func fetchContentPart(
	ctx context.Context, panicEvent string, fetch func() (*providercall.ContentFetchResponse, error),
) (resp *providercall.ContentFetchResponse, err error) {
	if panicked := runloop.Try(ctx, panicEvent, func() { resp, err = fetch() }); panicked != nil {
		return nil, panicked
	}
	return resp, err
}

func (h *DiscoveryHandler) handleArtistContent(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, h.artistSvc != nil,
		degradeUnserved(w, r),
		func(pn domain.ProviderName, provider, externalID string) {
			known := knownIDsParam(r)
			tracksLimit, ok := parseLimit(w, r, "tracks_limit", 5, 50, clampToMax)
			if !ok {
				return
			}
			albumsLimit, ok := parseLimit(w, r, "albums_limit", 100, 200, clampToMax)
			if !ok {
				return
			}

			tracksResp, albumsResp, tracksErr, albumsErr := h.fetchTopTracksAndAlbums(
				r.Context(), pn, externalID, known, tracksLimit, albumsLimit)
			if tracksErr != nil || albumsErr != nil {
				failContentFetch(w, r, errors.Join(tracksErr, albumsErr), "get artist content", provider, externalID)
				return
			}
			if failure := artistContentFailure(tracksResp, albumsResp); failure != nil {
				httputil.HandleServiceError(w, r, fmt.Errorf("artist content for %s %q: %w", provider, externalID, failure))
				return
			}

			dto := ArtistContentResponseDTO{
				TopTracks: contentFetchToDTO(tracksResp),
				Albums:    contentFetchToDTO(albumsResp),
			}
			if userId, hasUser := requestuser.UserIDFromContext(r.Context()); hasUser {
				h.ownership.StampOwnership(r.Context(), userId, ownableItems(dto.TopTracks.Items))
			}
			httputil.WriteJSON(w, http.StatusOK, dto)
		})
}
