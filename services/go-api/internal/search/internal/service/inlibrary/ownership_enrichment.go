package inlibrary

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/songfields"
	"context"
	"errors"
	"log/slog"
	"time"
)

const trackNumberFillTimeout = 10 * time.Second

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
	bg           *background.Tracker
}

var ErrCollaboratorRequired = errors.New("ownership enrichment needs its background tracker, ownership reader and track number filler, pass the absent filler to switch filling off")

type AbsentTrackNumberFiller struct{}

func (AbsentTrackNumberFiller) FillTrackNumber(context.Context, ids.UserId, ids.TrackId, int) error {
	return nil
}

func NewOwnershipEnrichmentService(
	bg *background.Tracker,
	ownership ports.OwnershipReader,
	trackNumbers ports.TrackNumberFiller,
) (*OwnershipEnrichmentService, error) {
	if bg == nil || ownership == nil || trackNumbers == nil {
		return nil, ErrCollaboratorRequired
	}
	return &OwnershipEnrichmentService{
		ownership:    ownership,
		trackNumbers: trackNumbers,
		bg:           bg,
	}, nil
}

func (s *OwnershipEnrichmentService) StampOwnership(
	ctx context.Context,
	userId ids.UserId,
	items []OwnableItem,
) {
	s.stampOwnedItems(ctx, userId, items)
}

func (s *OwnershipEnrichmentService) stampOwnedItems(
	ctx context.Context,
	userId ids.UserId,
	items []OwnableItem,
) map[string]ports.OwnedTrack {
	owned, err := s.ownership.OwnedByTitleArtist(ctx, userId)
	if err != nil {
		slog.WarnContext(ctx, "ownership.lookup_failed", logging.KeyError, err)
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
	itemISRC := songfields.CanonicalISRC(item.ISRC)
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
	userId ids.UserId,
	items []OwnableItem,
) <-chan struct{} {
	owned := s.stampOwnedItems(ctx, userId, items)
	return s.fillTrackNumbers(ctx, userId, pendingTrackNumbers(items, owned))
}

func (s *OwnershipEnrichmentService) fillTrackNumbers(
	ctx context.Context,
	userId ids.UserId,
	pending map[string]int,
) <-chan struct{} {
	done := make(chan struct{})
	if len(pending) == 0 {
		close(done)
		return done
	}

	launched := s.bg.LaunchWithin(ctx, "track_number.fill", trackNumberFillTimeout, func(bgCtx context.Context) {
		defer close(done)
		s.fillPending(bgCtx, userId, pending)
	})
	if !launched {
		close(done)
	}
	return done
}

func (s *OwnershipEnrichmentService) fillPending(
	ctx context.Context,
	userId ids.UserId,
	pending map[string]int,
) {
	for trackId, position := range pending {
		if ctx.Err() != nil {
			slog.WarnContext(ctx, "track_number.fill_abandoned",
				"pending", len(pending), logging.KeyError, ctx.Err())
			return
		}
		id, err := ids.ParseTrackId(trackId)
		if err != nil {
			slog.WarnContext(ctx, "track_number.fill_failed", logging.KeyTrackID, trackId, logging.KeyError, err)
			continue
		}
		if err := s.trackNumbers.FillTrackNumber(ctx, userId, id, position); err != nil {
			slog.WarnContext(ctx, "track_number.fill_failed", logging.KeyTrackID, trackId, logging.KeyError, err)
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
	stored := songfields.CanonicalISRC(track.ISRC)
	return songfields.IsCanonicalISRC(stored) && stored == songfields.CanonicalISRC(itemISRC)
}

func isSameNamedAlbum(storedAlbum, pageAlbum string) bool {
	storedKey := domain.OwnershipKey(storedAlbum, "")
	return storedKey != domain.OwnershipKey("", "") && storedKey == domain.OwnershipKey(pageAlbum, "")
}
