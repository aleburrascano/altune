package discoverycatalog

import (
	"altune/go-api/internal/catalog"
	catalogdomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"fmt"
)

type OwnedTrackRefLister interface {
	ListOwnedTrackRefs(ctx context.Context, userId shared.UserId) ([]catalogdomain.OwnedTrackRef, error)
}

type ownedTracks struct {
	refs OwnedTrackRefLister
}

func NewOwnedTracks(refs OwnedTrackRefLister) discovery.OwnedTrackLister {
	return ownedTracks{refs: refs}
}

func (o ownedTracks) ListOwnedTracks(ctx context.Context, userId shared.UserId) ([]discovery.OwnedTrack, error) {
	refs, err := o.refs.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		return nil, err
	}
	tracks := make([]discovery.OwnedTrack, len(refs))
	for i, ref := range refs {
		tracks[i] = discovery.OwnedTrack{
			TrackID:           ref.ID,
			Title:             ref.Title,
			Artist:            ref.Artist,
			AcquisitionStatus: ref.AcquisitionStatus,
			TrackNumber:       ref.TrackNumber,
			ISRC:              ref.ISRC,
			Album:             ref.Album,
		}
	}
	return tracks, nil
}

type TrackNumberSetter interface {
	Execute(ctx context.Context, userId shared.UserId, trackId catalog.TrackId, trackNumber int) (bool, error)
}

type trackNumberFiller struct {
	setter TrackNumberSetter
}

func NewTrackNumberFiller(setter TrackNumberSetter) discovery.TrackNumberFiller {
	return trackNumberFiller{setter: setter}
}

func (f trackNumberFiller) FillTrackNumber(ctx context.Context, userId shared.UserId, trackId string, trackNumber int) error {
	id, err := catalog.ParseTrackId(trackId)
	if err != nil {
		return fmt.Errorf("parse track id: %w", err)
	}
	if _, err := f.setter.Execute(ctx, userId, id, trackNumber); err != nil {
		return fmt.Errorf("fill track number: %w", err)
	}
	return nil
}
