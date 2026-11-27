package handler

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/internal/service/providerguard"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/parallel"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
	"fmt"
	"net/http"
)

type ContentFetchResponseDTO struct {
	Provider string            `json:"provider_name"`
	Status   string            `json:"status"`
	Items    []SearchResultDTO `json:"items"`
	Partial  bool              `json:"partial"`
	Code     string            `json:"code,omitempty"`
}

func contentFetchToDTO(resp *providerguard.ContentFetchResponse) ContentFetchResponseDTO {
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

var errContentUnserved = apierror.NewError(apierror.CodeDiscoveryContentUnserved, "")

func contentFetchFailure(resp *providerguard.ContentFetchResponse) *apierror.CodedError {
	if resp.Unserved {
		return errContentUnserved
	}
	switch resp.Status {
	case domain.ProviderStatusOK:
		return nil
	case domain.ProviderStatusTimeout:
		return apierror.NewError(apierror.CodeDiscoveryProviderTimeout, "")
	case domain.ProviderStatusRateLimited:
		return apierror.NewError(apierror.CodeDiscoveryProviderRateLimited, "")
	case domain.ProviderStatusCircuitOpen:
		return apierror.NewError(apierror.CodeDiscoveryProviderCircuitOpen, "")
	default:
		return apierror.NewError(apierror.CodeDiscoveryProviderError, "")
	}
}

func codeOf(failure *apierror.CodedError) string {
	if failure == nil {
		return ""
	}
	return failure.Code().String()
}

func failContentFetch(w http.ResponseWriter, r *http.Request, err error, operation, provider, externalID string) {
	httpserver.HandleServiceError(w, r, fmt.Errorf("%s for %s %q: %w", operation, provider, externalID, err))
}

func withProvider(
	w http.ResponseWriter,
	r *http.Request,
	call func(ctx context.Context, pn domain.ProviderName, provider, externalID string),
) {
	provider := httpserver.URLParam(r, "provider")
	pn, parseErr := domain.ParseProviderName(provider)
	if parseErr != nil {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryInvalidProvider, "unknown provider"))
		return
	}
	externalID, ok := externalIDParam(w, r)
	if !ok {
		return
	}
	call(r.Context(), pn, provider, externalID)
}

const maxAlbumTracks = 250

func (h *DiscoveryHandler) handleAlbumTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r,
		func(ctx context.Context, pn domain.ProviderName, provider, externalID string) {
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

			resp, err := h.albumSvc.ExecuteRequest(ctx, artistpage.AlbumTracksRequest{
				Provider:              pn,
				ExternalID:            externalID,
				Title:                 albumTitle,
				Artist:                albumArtist,
				MusicBrainzExternalID: albumMBID,
				Limit:                 limit,
				Known:                 knownIDsParam(r),
			})
			if err != nil {
				failContentFetch(w, r, err, "get album tracks", provider, externalID)
				return
			}

			if failure := contentFetchFailure(resp); failure != nil {
				httpserver.HandleServiceError(w, r, fmt.Errorf("album tracks for %s %q: %w", provider, externalID, failure))
				return
			}
			dto := contentFetchToDTO(resp)
			h.enrichAlbumOwnership(ctx, requestuser.UserID(r), albumTrackItems(albumTitle, dto.Items))
			httpserver.WriteJSON(w, http.StatusOK, dto)
		})
}

func (h *DiscoveryHandler) handleArtistTopTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r,
		func(ctx context.Context, pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 5, 50, clampToMax)
			if !ok {
				return
			}
			known := knownIDsParam(r)

			resp, err := h.artistSvc.GetTopTracks(ctx, pn, externalID, known, limit)
			if err != nil {
				failContentFetch(w, r, err, "get artist top tracks", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

func (h *DiscoveryHandler) handleArtistAlbums(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r,
		func(ctx context.Context, pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 50, 100, clampToMax)
			if !ok {
				return
			}
			known := knownIDsParam(r)

			resp, err := h.artistSvc.GetAlbums(ctx, pn, externalID, known, limit)
			if err != nil {
				failContentFetch(w, r, err, "get artist albums", provider, externalID)
				return
			}

			h.writeContentFetch(w, r, resp)
		})
}

func (h *DiscoveryHandler) handleRelatedTracks(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r,
		func(ctx context.Context, pn domain.ProviderName, provider, externalID string) {
			limit, ok := parseLimit(w, r, "limit", 20, 50, clampToMax)
			if !ok {
				return
			}

			resp, err := h.relatedSvc.Execute(ctx, pn, externalID, limit)
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

func artistContentFailure(tracks, albums *providerguard.ContentFetchResponse) *apierror.CodedError {
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
) (tracksResp, albumsResp *providerguard.ContentFetchResponse, tracksErr, albumsErr error) {
	g := parallel.New("artist_content.fetch", 2)
	g.Go(ctx, func() {
		tracksResp, tracksErr = fetchContentPart(ctx, "artist_content.top_tracks_panic", func() (*providerguard.ContentFetchResponse, error) {
			return h.artistSvc.GetTopTracks(ctx, pn, externalID, known, tracksLimit)
		})
	})
	g.Go(ctx, func() {
		albumsResp, albumsErr = fetchContentPart(ctx, "artist_content.albums_panic", func() (*providerguard.ContentFetchResponse, error) {
			return h.artistSvc.GetAlbums(ctx, pn, externalID, known, albumsLimit)
		})
	})
	_ = g.Wait()
	return tracksResp, albumsResp, tracksErr, albumsErr
}

func fetchContentPart(
	ctx context.Context, panicEvent string, fetch func() (*providerguard.ContentFetchResponse, error),
) (resp *providerguard.ContentFetchResponse, err error) {
	if panicked := background.SafeRun(ctx, panicEvent, func() { resp, err = fetch() }); panicked != nil {
		return nil, panicked
	}
	return resp, err
}

func (h *DiscoveryHandler) handleArtistContent(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r,
		func(ctx context.Context, pn domain.ProviderName, provider, externalID string) {
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
				ctx, pn, externalID, known, tracksLimit, albumsLimit)
			if tracksErr != nil || albumsErr != nil {
				failContentFetch(w, r, errors.Join(tracksErr, albumsErr), "get artist content", provider, externalID)
				return
			}
			if failure := artistContentFailure(tracksResp, albumsResp); failure != nil {
				httpserver.HandleServiceError(w, r, fmt.Errorf("artist content for %s %q: %w", provider, externalID, failure))
				return
			}

			dto := ArtistContentResponseDTO{
				TopTracks: contentFetchToDTO(tracksResp),
				Albums:    contentFetchToDTO(albumsResp),
			}
			h.stampOwnership(ctx, requestuser.UserID(r), ownableItems(dto.TopTracks.Items))
			httpserver.WriteJSON(w, http.StatusOK, dto)
		})
}
