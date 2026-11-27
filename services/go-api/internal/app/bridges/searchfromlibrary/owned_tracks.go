package searchfromlibrary

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/search"
	"altune/go-api/internal/shared/ids"
	"context"
	"fmt"
)

type OwnedTrackRefLister interface {
	ListOwnedTrackRefs(ctx context.Context, userId ids.UserId) ([]library.OwnedTrackRef, error)
}

type ownedTracks struct {
	refs OwnedTrackRefLister
}

func NewOwnedTracks(refs OwnedTrackRefLister) search.OwnedTrackLister {
	return ownedTracks{refs: refs}
}

func (o ownedTracks) ListOwnedTracks(ctx context.Context, userId ids.UserId) ([]search.OwnedTrack, error) {
	refs, err := o.refs.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("list owned track refs: %w", err)
	}
	tracks := make([]search.OwnedTrack, len(refs))
	for i, ref := range refs {
		tracks[i] = search.OwnedTrack{
			TrackID:           ref.ID,
			Title:             ref.Title,
			Artist:            ref.Artist,
			AcquisitionStatus: ref.DownloadStatus,
			TrackNumber:       ref.TrackNumber,
			ISRC:              ref.ISRC,
			Album:             ref.Album,
		}
	}
	return tracks, nil
}

type TrackNumberSetter interface {
	Execute(ctx context.Context, userId ids.UserId, trackId library.TrackId, trackNumber int) (bool, error)
}

type trackNumberFiller struct {
	setter TrackNumberSetter
}

func NewTrackNumberFiller(setter TrackNumberSetter) search.TrackNumberFiller {
	return trackNumberFiller{setter: setter}
}

func (f trackNumberFiller) FillTrackNumber(ctx context.Context, userId ids.UserId, trackId ids.TrackId, trackNumber int) error {
	if _, err := f.setter.Execute(ctx, userId, trackId, trackNumber); err != nil {
		return fmt.Errorf("fill track number: %w", err)
	}
	return nil
}
