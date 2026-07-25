package catalogbridge

import (
	"context"
	"fmt"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

var _ ports.OwnershipReader = (*OwnershipReader)(nil)

type ownedTrackLister interface {
	ListOwnedTrackRefs(ctx context.Context, userId shared.UserId) ([]catalogDomain.OwnedTrackRef, error)
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
		key := ports.OwnershipKey(ref.Title, ref.Artist)
		if _, taken := owned[key]; taken {
			continue
		}
		owned[key] = ports.OwnedTrack{
			TrackID:           ref.ID,
			AcquisitionStatus: ref.AcquisitionStatus,
			TrackNumber:       ref.TrackNumber,
		}
	}
	return owned, nil
}

var _ ports.TrackNumberFiller = (*TrackNumberWriter)(nil)

type trackNumberSetter interface {
	Execute(ctx context.Context, userId shared.UserId, trackId catalogDomain.TrackId, trackNumber int) (bool, error)
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
	id, err := catalogDomain.ParseTrackId(trackId)
	if err != nil {
		return nil
	}
	if _, err := w.setter.Execute(ctx, userId, id, trackNumber); err != nil {
		return fmt.Errorf("fill track number: %w", err)
	}
	return nil
}
