package catalogbridge

import (
	"context"
	"fmt"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type trackReader interface {
	GetByID(ctx context.Context, id catalogDomain.TrackId, userId shared.UserId) (*catalogDomain.Track, error)
}

type NowPlayingReader struct {
	tracks trackReader
}

func NewNowPlayingReader(tracks trackReader) *NowPlayingReader {
	return &NowPlayingReader{tracks: tracks}
}

func trackAbsent() (*ports.NowPlayingTrack, error) {
	return nil, nil
}

func (r *NowPlayingReader) Lookup(
	ctx context.Context,
	userId shared.UserId,
	trackId string,
) (*ports.NowPlayingTrack, error) {
	id, err := catalogDomain.ParseTrackId(trackId)
	if err != nil {
		return trackAbsent()
	}

	track, err := r.tracks.GetByID(ctx, id, userId)
	if err != nil {
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	if track == nil {
		return trackAbsent()
	}

	return &ports.NowPlayingTrack{
		Id:                track.ID.String(),
		Title:             track.Title,
		Artist:            track.Artist,
		ArtworkURL:        track.ArtworkURL,
		DurationSeconds:   track.DurationSeconds,
		AcquisitionStatus: track.AcquisitionStatus.String(),
	}, nil
}
