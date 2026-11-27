package playbackfromlibrary

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
)

type CatalogTracks interface {
	GetByID(ctx context.Context, id library.TrackId, userId ids.UserId) (*library.Track, error)
}

type nowPlayingTracks struct {
	tracks CatalogTracks
}

func NewNowPlayingTracks(tracks CatalogTracks) playback.TrackSource {
	return nowPlayingTracks{tracks: tracks}
}

func (s nowPlayingTracks) NowPlayingTrack(ctx context.Context, userId ids.UserId, id ids.TrackId) (*playback.NowPlayingTrack, error) {
	track, err := s.tracks.GetByID(ctx, id, userId)
	if errors.Is(err, library.ErrTrackNotFound) {
		return nil, playback.ErrNowPlayingTrackNotFound
	}
	if errors.Is(err, library.ErrDBTransient) {
		return nil, fmt.Errorf("%w: %w", playback.ErrTrackSourceUnavailable, err)
	}
	if err != nil {
		return nil, fmt.Errorf("get by id: %w", err)
	}
	return &playback.NowPlayingTrack{
		Id:                track.ID().String(),
		Title:             track.Title(),
		Artist:            track.Artist(),
		ArtworkURL:        track.ArtworkURL(),
		DurationSeconds:   track.DurationSeconds(),
		AcquisitionStatus: track.DownloadStatus().String(),
	}, nil
}
