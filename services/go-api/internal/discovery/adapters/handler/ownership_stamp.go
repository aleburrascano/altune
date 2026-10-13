package handler

import (
	"altune/go-api/internal/discovery/adapters/authbridge"
	"altune/go-api/internal/discovery/service/ownership"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

func (h *DiscoveryHandler) writeContentFetch(
	w http.ResponseWriter,
	r *http.Request,
	resp *providercall.ContentFetchResponse,
) {
	dto := contentFetchToDTO(resp)
	if userId, ok := authbridge.UserIDFromContext(r.Context()); ok {
		h.ownership.StampOwnership(r.Context(), userId, ownableItems(dto.Items))
	}
	status, _ := contentFetchOutcome(resp)
	httputil.WriteJSON(w, status, dto)
}

func (h *DiscoveryHandler) WithOwnershipEnrichment(svc *ownership.OwnershipEnrichmentService) *DiscoveryHandler {
	h.ownership = svc
	return h
}

func ownableItems(slates ...[]SearchResultDTO) []ownership.OwnableItem {
	var out []ownership.OwnableItem
	for _, items := range slates {
		for i := range items {
			out = append(out, ownableItem(&items[i]))
		}
	}
	return out
}

func ownableItem(dto *SearchResultDTO) ownership.OwnableItem {
	return ownership.OwnableItem{Kind: dto.Kind, Title: dto.Title, Artist: dto.Subtitle, Extras: &dto.Extras}
}

func ownershipTargets(
	results []SearchResultDTO,
	topResult *SearchResultDTO,
	sections []ResultSectionDTO,
) []ownership.OwnableItem {
	targets := ownableItems(results)
	if topResult != nil {
		targets = append(targets, ownableItem(topResult))
	}
	for _, s := range sections {
		targets = append(targets, ownableItems(s.Items)...)
	}
	return targets
}

func blendedSlateToDTOs(slate search.BlendedSlate) (*SearchResultDTO, []ResultSectionDTO) {
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
