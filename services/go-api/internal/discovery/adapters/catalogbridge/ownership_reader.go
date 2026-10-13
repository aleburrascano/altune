package catalogbridge

import (
	catalogdomain "altune/go-api/internal/catalog/domain"
	trackservice "altune/go-api/internal/catalog/service/track"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

var _ ports.OwnershipReader = (*OwnershipReader)(nil)

type ownedTrackLister interface {
	ListOwnedTrackRefs(ctx context.Context, userId shared.UserId) ([]catalogdomain.OwnedTrackRef, error)
}

type OwnershipReader struct {
	tracks ownedTrackLister
}

func NewOwnershipReader(tracks ownedTrackLister) *OwnershipReader {
	return &OwnershipReader{tracks: tracks}
}

func (r *OwnershipReader) OwnedByTitleArtist(
	ctx context.Context,
	userId shared.UserId,
) (map[string]ports.OwnedTrack, error) {
	refs, err := r.tracks.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("read owned tracks: %w", err)
	}

	owned := make(map[string]ports.OwnedTrack, len(refs))
	for _, ref := range refs {
		track := ports.OwnedTrack{
			TrackID:           ref.ID,
			Title:             ref.Title,
			Artist:            ref.Artist,
			AcquisitionStatus: ref.AcquisitionStatus,
			TrackNumber:       ref.TrackNumber,
		}
		key := domain.OwnershipKey(track.Title, track.Artist)
		if _, taken := owned[key]; taken {
			continue
		}
		owned[key] = track
	}
	return owned, nil
}

var (
	_ ports.TrackNumberFiller = (*TrackNumberWriter)(nil)
	_ trackNumberSetter       = (*trackservice.SetTrackNumberService)(nil)
)

type trackNumberSetter interface {
	Execute(ctx context.Context, userId shared.UserId, trackId catalogdomain.TrackId, trackNumber int) (bool, error)
}

type TrackNumberWriter struct {
	setter trackNumberSetter
}

func NewTrackNumberWriter(setter trackNumberSetter) *TrackNumberWriter {
	return &TrackNumberWriter{setter: setter}
}

func (w *TrackNumberWriter) FillTrackNumber(
	ctx context.Context,
	userId shared.UserId,
	trackId string,
	trackNumber int,
) error {
	id, err := catalogdomain.ParseTrackId(trackId)
	if err != nil {
		return fmt.Errorf("parse track id: %w", err)
	}
	if _, err := w.setter.Execute(ctx, userId, id, trackNumber); err != nil {
		return fmt.Errorf("fill track number: %w", err)
	}
	return nil
}
