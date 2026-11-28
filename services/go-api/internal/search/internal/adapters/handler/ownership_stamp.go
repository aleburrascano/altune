package handler

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"context"
	"fmt"
	"net/http"
)

func (h *DiscoveryHandler) writeContentFetch(
	w http.ResponseWriter,
	r *http.Request,
	resp *service.ContentFetchResponse,
) {
	if failure := contentFetchFailure(resp); failure != nil {
		httpserver.HandleServiceError(w, r, fmt.Errorf("%s content: %w", resp.ProviderName, failure))
		return
	}
	dto := contentFetchToDTO(resp)
	h.stampOwnership(r.Context(), requestuser.UserID(r), ownableItems(dto.Items))
	httpserver.WriteJSON(w, http.StatusOK, dto)
}

type OwnershipStamper interface {
	StampOwnership(ctx context.Context, userId ids.UserId, items []service.OwnableItem)
	EnrichAlbumTracks(ctx context.Context, userId ids.UserId, items []service.OwnableItem) <-chan struct{}
}

type NoOwnership struct{}

func (NoOwnership) StampOwnership(context.Context, ids.UserId, []service.OwnableItem) {}

func (NoOwnership) EnrichAlbumTracks(context.Context, ids.UserId, []service.OwnableItem) <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (h *DiscoveryHandler) AttachOwnership(stamper OwnershipStamper) error {
	if stamper == nil {
		return domain.ErrOwnershipRequired
	}
	h.ownership = stamper
	return nil
}

func (h *DiscoveryHandler) stampOwnership(ctx context.Context, userId ids.UserId, items []service.OwnableItem) {
	h.ownership.StampOwnership(ctx, userId, items)
}

func (h *DiscoveryHandler) enrichAlbumOwnership(ctx context.Context, userId ids.UserId, items []service.OwnableItem) {
	_ = h.ownership.EnrichAlbumTracks(ctx, userId, items)
}

func ownableItems(slates ...[]SearchResultDTO) []service.OwnableItem {
	var out []service.OwnableItem
	for _, items := range slates {
		for i := range items {
			out = append(out, ownableItem(&items[i]))
		}
	}
	return out
}

func albumTrackItems(albumTitle string, items []SearchResultDTO) []service.OwnableItem {
	out := ownableItems(items)
	for i := range out {
		out[i].Album = albumTitle
	}
	return out
}

func ownableItem(dto *SearchResultDTO) service.OwnableItem {
	return service.OwnableItem{Kind: dto.Kind, Title: dto.Title, Artist: dto.Subtitle, ISRC: dto.isrc, TrackPosition: dto.position, Extras: &dto.Extras}
}

func ownershipTargets(
	results []SearchResultDTO,
	topResult *SearchResultDTO,
	sections []ResultSectionDTO,
) []service.OwnableItem {
	targets := ownableItems(results)
	if topResult != nil {
		targets = append(targets, ownableItem(topResult))
	}
	for _, s := range sections {
		targets = append(targets, ownableItems(s.Items)...)
	}
	return targets
}

func blendedSlateToDTOs(slate service.BlendedSlate) (*SearchResultDTO, []ResultSectionDTO) {
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
