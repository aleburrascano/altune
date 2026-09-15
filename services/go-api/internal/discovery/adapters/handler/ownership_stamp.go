package handler

import (
	"context"
	"log/slog"
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
)

func (h *DiscoveryHandler) writeContentFetch(
	w http.ResponseWriter,
	r *http.Request,
	resp *service.ContentFetchResponse,
) {
	dto := contentFetchToDTO(resp)
	if userId, ok := auth.UserIDFromContext(r.Context()); ok {
		h.stampOwnership(r.Context(), userId, dto.Items)
	}
	status, _ := contentFetchOutcome(resp)
	httputil.WriteJSON(w, status, dto)
}

func (h *DiscoveryHandler) WithOwnership(reader ports.OwnershipReader) *DiscoveryHandler {
	h.ownership = reader
	return h
}

func (h *DiscoveryHandler) WithTrackNumberFiller(filler ports.TrackNumberFiller) *DiscoveryHandler {
	h.trackNumbers = filler
	return h
}

// fillAlbumTrackNumbers backfills track positions for owned tracks in a
// detached, fire-and-forget goroutine. The returned channel is closed once
// that work has finished (after any panic has been recovered), or immediately
// when there is nothing to fill, so callers and tests can wait on it.
func (h *DiscoveryHandler) fillAlbumTrackNumbers(
	ctx context.Context,
	userId shared.UserId,
	items []SearchResultDTO,
) <-chan struct{} {
	done := make(chan struct{})
	if h.trackNumbers == nil || h.ownership == nil {
		close(done)
		return done
	}

	pending := map[string]int{}
	for i := range items {
		trackId, ok := items[i].Extras["owned_track_id"].(string)
		if !ok || trackId == "" {
			continue
		}
		if _, positioned := items[i].Extras["track_position"]; positioned {
			continue
		}
		pending[trackId] = i + 1
	}
	if len(pending) == 0 {
		close(done)
		return done
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		// done is closed only after the fill returns normally or its panic is
		// recovered; an unrecovered panic crashes before signalling completion.
		func() {
			defer service.RecoverGoroutine(detached, "track_number.fill_panic")
			for trackId, position := range pending {
				if err := h.trackNumbers.FillTrackNumber(detached, userId, trackId, position); err != nil {
					slog.WarnContext(detached, "track_number.fill_failed",
						"track_id", trackId, "error", err)
				}
			}
		}()
		close(done)
	}()
	return done
}

func (h *DiscoveryHandler) stampOwnership(
	ctx context.Context,
	userId shared.UserId,
	slates ...[]SearchResultDTO,
) {
	if h.ownership == nil {
		return
	}

	owned, err := h.ownership.OwnedByTitleArtist(ctx, userId)
	if err != nil {
		slog.WarnContext(ctx, "ownership.lookup_failed", "error", err)
		return
	}
	if len(owned) == 0 {
		return
	}

	for _, results := range slates {
		for i := range results {
			if results[i].Kind != "track" {
				continue
			}
			match, ok := owned[ports.OwnershipKey(results[i].Title, results[i].Subtitle)]
			if !ok {
				continue
			}
			if results[i].Extras == nil {
				results[i].Extras = map[string]any{}
			}
			results[i].Extras["owned_track_id"] = match.TrackID
			results[i].Extras["owned_acquisition_status"] = match.AcquisitionStatus
		}
	}
}

func ownershipTargets(
	results []SearchResultDTO,
	topResult *SearchResultDTO,
	sections []ResultSectionDTO,
) [][]SearchResultDTO {
	targets := [][]SearchResultDTO{results}
	if topResult != nil {
		targets = append(targets, []SearchResultDTO{*topResult})
	}
	for _, s := range sections {
		targets = append(targets, s.Items)
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
