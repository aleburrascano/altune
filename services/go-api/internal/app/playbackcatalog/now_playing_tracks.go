package playbackcatalog

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/playback"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
)

type CatalogTracks interface {
	GetByID(ctx context.Context, id catalog.TrackId, userId shared.UserId) (*catalog.Track, error)
}

type nowPlayingTracks struct {
	tracks CatalogTracks
}

func NewNowPlayingTracks(tracks CatalogTracks) playback.TrackSource {
	return nowPlayingTracks{tracks: tracks}
}

func (s nowPlayingTracks) NowPlayingTrack(ctx context.Context, userId shared.UserId, id shared.TrackId) (*playback.NowPlayingTrack, error) {
	track, err := s.tracks.GetByID(ctx, id, userId)
	if errors.Is(err, catalog.ErrDBTransient) {
		return nil, fmt.Errorf("%w: %w", playback.ErrTrackSourceUnavailable, err)
	}
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, playback.ErrNowPlayingTrackNotFound
	}
	return &playback.NowPlayingTrack{
		Id:                track.ID.String(),
		Title:             track.Title,
		Artist:            track.Artist,
		ArtworkURL:        track.ArtworkURL,
		DurationSeconds:   track.DurationSeconds,
		AcquisitionStatus: track.AcquisitionStatus.String(),
	}, nil
}
