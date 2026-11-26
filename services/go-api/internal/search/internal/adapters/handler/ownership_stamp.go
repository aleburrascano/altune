package handler

import (
	"altune/go-api/internal/search/internal/service/inlibrary"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/search/internal/service/query"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"errors"
	"fmt"
	"net/http"
)

func (h *DiscoveryHandler) writeContentFetch(
	w http.ResponseWriter,
	r *http.Request,
	resp *providerguard.ContentFetchResponse,
) {
	if failure := contentFetchFailure(resp); failure != nil {
		httpserver.HandleServiceError(w, r, fmt.Errorf("%s content: %w", resp.ProviderName, failure))
		return
	}
	dto := contentFetchToDTO(resp)
	if userId, ok := requestuser.UserIDFromContext(r.Context()); ok {
		h.stampOwnership(r.Context(), userId, ownableItems(dto.Items))
	}
	httpserver.WriteJSON(w, http.StatusOK, dto)
}

type OwnershipStamper interface {
	StampOwnership(ctx context.Context, userId ids.UserId, items []inlibrary.OwnableItem)
	EnrichAlbumTracks(ctx context.Context, userId ids.UserId, items []inlibrary.OwnableItem) <-chan struct{}
}

type NoOwnership struct{}

func (NoOwnership) StampOwnership(context.Context, ids.UserId, []inlibrary.OwnableItem) {}

func (NoOwnership) EnrichAlbumTracks(context.Context, ids.UserId, []inlibrary.OwnableItem) <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

var ErrOwnershipRequired = errors.New("handler: the ownership stamper is required, pass NoOwnership to switch it off")

func (h *DiscoveryHandler) AttachOwnership(stamper OwnershipStamper) error {
	if stamper == nil {
		return ErrOwnershipRequired
	}
	h.ownership = stamper
	return nil
}

func (h *DiscoveryHandler) stampOwnership(ctx context.Context, userId ids.UserId, items []inlibrary.OwnableItem) {
	h.ownership.StampOwnership(ctx, userId, items)
}

func (h *DiscoveryHandler) enrichAlbumOwnership(ctx context.Context, userId ids.UserId, items []inlibrary.OwnableItem) {
	_ = h.ownership.EnrichAlbumTracks(ctx, userId, items)
}

func ownableItems(slates ...[]SearchResultDTO) []inlibrary.OwnableItem {
	var out []inlibrary.OwnableItem
	for _, items := range slates {
		for i := range items {
			out = append(out, ownableItem(&items[i]))
		}
	}
	return out
}

func albumTrackItems(albumTitle string, items []SearchResultDTO) []inlibrary.OwnableItem {
	out := ownableItems(items)
	for i := range out {
		out[i].Album = albumTitle
	}
	return out
}

func ownableItem(dto *SearchResultDTO) inlibrary.OwnableItem {
	return inlibrary.OwnableItem{Kind: dto.Kind, Title: dto.Title, Artist: dto.Subtitle, ISRC: dto.isrc, TrackPosition: dto.position, Extras: &dto.Extras}
}

func ownershipTargets(
	results []SearchResultDTO,
	topResult *SearchResultDTO,
	sections []ResultSectionDTO,
) []inlibrary.OwnableItem {
	targets := ownableItems(results)
	if topResult != nil {
		targets = append(targets, ownableItem(topResult))
	}
	for _, s := range sections {
		targets = append(targets, ownableItems(s.Items)...)
	}
	return targets
}

func blendedSlateToDTOs(slate query.BlendedSlate) (*SearchResultDTO, []ResultSectionDTO) {
	sections := make([]ResultSectionDTO, 0, len(slate.Sections))
	for _, s := range slate.Sections {
		sections = append(sections, ResultSectionDTO{
			Kind:    s.Kind.String(),
			Items:   searchResultsToDTOs(s.Items),
			HasMore: s.HasMore,
		})
	}
	if slate.TopResult == nil {
		return nil, sections
	}
	top := searchResultToDTO(*slate.TopResult)
	return &top, sections
}
