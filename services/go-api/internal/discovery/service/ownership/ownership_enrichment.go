package ownership

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/runloop"
	"altune/go-api/internal/shared/songfacts"
	"context"
	"log/slog"
	"time"
)

var trackNumberFillTimeout = 10 * time.Second

type OwnableItem struct {
	Kind          string
	Title         string
	Artist        string
	Album         string
	ISRC          string
	TrackPosition domain.TrackPosition
	Extras        *map[string]any
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
	bg           *runloop.Tracker
}

func NewOwnershipEnrichmentService(
	ownership ports.OwnershipReader,
	trackNumbers ports.TrackNumberFiller,
) *OwnershipEnrichmentService {
	return &OwnershipEnrichmentService{
		ownership:    ownership,
		trackNumbers: trackNumbers,
		bg:           &runloop.Tracker{},
	}
}

func (s *OwnershipEnrichmentService) WaitForBackground() {
	if s == nil {
		return
	}
	s.bg.Wait()
}

func (s *OwnershipEnrichmentService) StampOwnership(
	ctx context.Context,
	userId shared.UserId,
	items []OwnableItem,
) {
	s.stampOwnedItems(ctx, userId, items)
}

func (s *OwnershipEnrichmentService) stampOwnedItems(
	ctx context.Context,
	userId shared.UserId,
	items []OwnableItem,
) map[string]ports.OwnedTrack {
	if s == nil || s.ownership == nil {
		return nil
	}

	owned, err := s.ownership.OwnedByTitleArtist(ctx, userId)
	if err != nil {
		slog.WarnContext(ctx, "ownership.lookup_failed", "error", err)
		return nil
	}
	for _, item := range items {
		stampOwned(item, owned)
	}
	return owned
}

func stampOwned(item OwnableItem, owned map[string]ports.OwnedTrack) {
	if item.Kind != domain.ResultKindTrack.String() || item.Extras == nil {
		return
	}
	match, ok := ownedMatch(item, owned)
	if !ok {
		return
	}
	if *item.Extras == nil {
		*item.Extras = map[string]any{}
	}
	(*item.Extras)[domain.ExtraOwnedTrackID] = match.TrackID
	(*item.Extras)["owned_acquisition_status"] = match.AcquisitionStatus
}

func ownedMatch(item OwnableItem, owned map[string]ports.OwnedTrack) (ports.OwnedTrack, bool) {
	itemISRC := songfacts.CanonicalISRC(item.ISRC)
	if itemISRC != "" {
		if match, ok := owned[domain.OwnershipISRCKey(itemISRC)]; ok {
			return match, true
		}
	}
	match, ok := owned[domain.OwnershipKey(item.Title, item.Artist)]
	if !ok || (itemISRC != "" && match.ISRC != "" && match.ISRC != itemISRC) {
		return ports.OwnedTrack{}, false
	}
	return match, true
}

func (s *OwnershipEnrichmentService) EnrichAlbumTracks(
	ctx context.Context,
	userId shared.UserId,
	items []OwnableItem,
) <-chan struct{} {
	owned := s.stampOwnedItems(ctx, userId, items)
	return s.fillTrackNumbers(ctx, userId, pendingTrackNumbers(items, owned))
}

func (s *OwnershipEnrichmentService) fillTrackNumbers(
	ctx context.Context,
	userId shared.UserId,
	pending map[string]int,
) <-chan struct{} {
	done := make(chan struct{})
	if s == nil || s.trackNumbers == nil || len(pending) == 0 {
		close(done)
		return done
	}

	s.bg.Launch(ctx, "track_number.fill", func(bgCtx context.Context) {
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

func pendingTrackNumbers(items []OwnableItem, owned map[string]ports.OwnedTrack) map[string]int {
	byTrackID := make(map[string]ports.OwnedTrack, len(owned))
	for _, track := range owned {
		byTrackID[track.TrackID] = track
	}
	pending := map[string]int{}
	for _, item := range items {
		extras := item.extras()
		track, isOwned := byTrackID[domain.StringExtra(extras, domain.ExtraOwnedTrackID)]
		if !isOwned || track.TrackNumber != nil {
			continue
		}
		position := item.TrackPosition.Track
		if position <= 0 || !matchesByISRC(track, item.ISRC) || !isSameNamedAlbum(track.Album, item.Album) {
			continue
		}
		pending[track.TrackID] = position
	}
	return pending
}

func matchesByISRC(track ports.OwnedTrack, itemISRC string) bool {
	stored := songfacts.CanonicalISRC(track.ISRC)
	return songfacts.IsCanonicalISRC(stored) && stored == songfacts.CanonicalISRC(itemISRC)
}

func isSameNamedAlbum(storedAlbum, pageAlbum string) bool {
	storedKey := domain.OwnershipKey(storedAlbum, "")
	return storedKey != domain.OwnershipKey("", "") && storedKey == domain.OwnershipKey(pageAlbum, "")
}
