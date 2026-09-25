package catalogbridge

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
)

var _ ports.OwnershipReader = (*OwnershipReader)(nil)

type ownedTrackLister interface {
	ListOwnedTracks(ctx context.Context, userId shared.UserId) ([]ports.OwnedTrack, error)
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
	tracks, err := r.tracks.ListOwnedTracks(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("read owned tracks: %w", err)
	}

	owned := make(map[string]ports.OwnedTrack, len(tracks))
	for _, track := range tracks {
		key := ports.OwnershipKey(track.Title, track.Artist)
		if _, taken := owned[key]; taken {
			continue
		}
		owned[key] = track
	}
	return owned, nil
}

var _ ports.TrackNumberFiller = (*TrackNumberWriter)(nil)

type trackNumberSetter interface {
	Execute(ctx context.Context, userId shared.UserId, trackId string, trackNumber int) (bool, error)
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
	// The track id crosses as a string; the catalog side of the seam parses it
	// to a domain TrackId (see app wiring). A malformed persisted id surfaces as
	// an error there so the batch caller's per-track warn log fires instead of
	// silently dropping the fill; that caller tolerates the error and moves on.
	if _, err := w.setter.Execute(ctx, userId, trackId, trackNumber); err != nil {
		return fmt.Errorf("fill track number: %w", err)
	}
	return nil
}
