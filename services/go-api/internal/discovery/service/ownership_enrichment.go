package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
)

// OwnableItem is the view of one response item that ownership enrichment reads
// and stamps. Extras points at the item's extras map, so a nil map is
// allocated in place on the caller's item.
type OwnableItem struct {
	Kind   string
	Title  string
	Artist string
	Extras *map[string]any
}

// extras returns the item's extras map, or nil when it has none.
func (it OwnableItem) extras() map[string]any {
	if it.Extras == nil {
		return nil
	}
	return *it.Extras
}

// OwnershipEnrichmentService marks response items the user already owns and
// backfills album positions onto owned tracks that lack one. A nil service, or
// one built without an ownership reader, enriches nothing.
type OwnershipEnrichmentService struct {
	ownership    ports.OwnershipReader
	trackNumbers ports.TrackNumberFiller
}

func NewOwnershipEnrichmentService(
	ownership ports.OwnershipReader,
	trackNumbers ports.TrackNumberFiller,
) *OwnershipEnrichmentService {
	return &OwnershipEnrichmentService{ownership: ownership, trackNumbers: trackNumbers}
}

// StampOwnership sets owned_track_id and owned_acquisition_status on every
// track item matching one of the user's owned tracks by title and artist. A
// failed ownership lookup is logged and leaves the items untouched.
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
	(*item.Extras)["owned_track_id"] = match.TrackID
	(*item.Extras)["owned_acquisition_status"] = match.AcquisitionStatus
}

// EnrichAlbumTracks stamps ownership onto an album's track list, then
// backfills the album position of each owned track that lacks one, taking the
// position from the item's 1-based index. The backfill runs in a detached,
// fire-and-forget goroutine; the returned channel is closed once that work has
// finished (after any panic has been recovered), or immediately when there is
// nothing to fill, so callers and tests can wait on it.
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

	detached := context.WithoutCancel(ctx)
	go func() {
		// done is closed only after the fill returns normally or its panic is
		// recovered; an unrecovered panic crashes before signalling completion.
		func() {
			defer RecoverGoroutine(detached, "track_number.fill_panic")
			for trackId, position := range pending {
				if err := s.trackNumbers.FillTrackNumber(detached, userId, trackId, position); err != nil {
					slog.WarnContext(detached, "track_number.fill_failed",
						"track_id", trackId, "error", err)
				}
			}
		}()
		close(done)
	}()
	return done
}

// pendingTrackNumbers maps each owned, unpositioned track id to its 1-based
// position in items.
func pendingTrackNumbers(items []OwnableItem) map[string]int {
	pending := map[string]int{}
	for i, item := range items {
		extras := item.extras()
		trackId, ok := extras["owned_track_id"].(string)
		if !ok || trackId == "" {
			continue
		}
		if _, positioned := extras["track_position"]; positioned {
			continue
		}
		pending[trackId] = i + 1
	}
	return pending
}
