package handler

import (
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/httputil"
)

func (h *DiscoveryHandler) writeContentFetch(
	w http.ResponseWriter,
	r *http.Request,
	resp *service.ContentFetchResponse,
) {
	dto := contentFetchToDTO(resp)
	if userId, ok := auth.UserIDFromContext(r.Context()); ok {
		h.ownership.StampOwnership(r.Context(), userId, ownableItems(dto.Items))
	}
	status, _ := contentFetchOutcome(resp)
	httputil.WriteJSON(w, status, dto)
}

func (h *DiscoveryHandler) WithOwnershipEnrichment(svc *service.OwnershipEnrichmentService) *DiscoveryHandler {
	h.ownership = svc
	return h
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

func ownableItem(dto *SearchResultDTO) service.OwnableItem {
	return service.OwnableItem{Kind: dto.Kind, Title: dto.Title, Artist: dto.Subtitle, Extras: &dto.Extras}
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
