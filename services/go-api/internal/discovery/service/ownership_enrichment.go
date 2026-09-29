package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
	"time"
)

var trackNumberFillTimeout = 10 * time.Second

type OwnableItem struct {
	Kind   string
	Title  string
	Artist string
	Extras *map[string]any
}

func (it OwnableItem) extras() map[string]any {
	if it.Extras == nil {
		return nil
	}
	return *it.Extras
}

type OwnershipEnrichmentService struct {
	ownership    ports.OwnershipReader
	trackNumbers ports.TrackNumberFiller
	bg           *backgroundRunner
}

func NewOwnershipEnrichmentService(
	ownership ports.OwnershipReader,
	trackNumbers ports.TrackNumberFiller,
) *OwnershipEnrichmentService {
	return &OwnershipEnrichmentService{
		ownership:    ownership,
		trackNumbers: trackNumbers,
		bg:           &backgroundRunner{},
	}
}

func (s *OwnershipEnrichmentService) WaitForBackground() {
	if s == nil {
		return
	}
	s.bg.wait()
}

func (s *OwnershipEnrichmentService) StampOwnership(
	ctx context.Context,
	userId shared.UserId,
	items []OwnableItem,
) {
	if s == nil || s.ownership == nil {
		return
	}

	owned, err := s.ownership.OwnedByTitleArtist(ctx, userId)
	if err != nil {
		slog.WarnContext(ctx, "ownership.lookup_failed", "error", err)
		return
	}
	if len(owned) == 0 {
		return
	}

	for _, item := range items {
		stampOwned(item, owned)
	}
}

func stampOwned(item OwnableItem, owned map[string]ports.OwnedTrack) {
	if item.Kind != domain.ResultKindTrack.String() || item.Extras == nil {
		return
	}
	match, ok := owned[ports.OwnershipKey(item.Title, item.Artist)]
	if !ok {
		return
	}
	if *item.Extras == nil {
		*item.Extras = map[string]any{}
	}
	(*item.Extras)[domain.ExtraOwnedTrackID] = match.TrackID
	(*item.Extras)["owned_acquisition_status"] = match.AcquisitionStatus
}

func (s *OwnershipEnrichmentService) EnrichAlbumTracks(
	ctx context.Context,
	userId shared.UserId,
	items []OwnableItem,
) <-chan struct{} {
	s.StampOwnership(ctx, userId, items)
	return s.fillTrackNumbers(ctx, userId, items)
}

func (s *OwnershipEnrichmentService) fillTrackNumbers(
	ctx context.Context,
	userId shared.UserId,
	items []OwnableItem,
) <-chan struct{} {
	done := make(chan struct{})
	if s == nil || s.trackNumbers == nil || s.ownership == nil {
		close(done)
		return done
	}

	pending := pendingTrackNumbers(items)
	if len(pending) == 0 {
		close(done)
		return done
	}

	s.bg.launch(ctx, "track_number.fill", func(bgCtx context.Context) {
		defer close(done)
		fillCtx, cancel := context.WithTimeout(bgCtx, trackNumberFillTimeout)
		defer cancel()
		s.fillPending(fillCtx, userId, pending)
	})
	return done
}

func (s *OwnershipEnrichmentService) fillPending(
	ctx context.Context,
	userId shared.UserId,
	pending map[string]int,
) {
	for trackId, position := range pending {
		if ctx.Err() != nil {
			slog.WarnContext(ctx, "track_number.fill_abandoned",
				"pending", len(pending), "error", ctx.Err())
			return
		}
		if err := s.trackNumbers.FillTrackNumber(ctx, userId, trackId, position); err != nil {
			slog.WarnContext(ctx, "track_number.fill_failed", "track_id", trackId, "error", err)
		}
	}
}

func pendingTrackNumbers(items []OwnableItem) map[string]int {
	pending := map[string]int{}
	for i, item := range items {
		extras := item.extras()
		trackId, ok := extras[domain.ExtraOwnedTrackID].(string)
		if !ok || trackId == "" {
			continue
		}
		if _, positioned := extras[domain.ExtraTrackPosition]; positioned {
			continue
		}
		pending[trackId] = i + 1
	}
	return pending
}
