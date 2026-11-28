package service

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type NowPlayingReader struct {
	tracks ports.TrackSource
}

var ErrNowPlayingCollaboratorRequired = errors.New("nowplaying: a required collaborator is missing")

func NewNowPlayingReader(tracks ports.TrackSource) (*NowPlayingReader, error) {
	if tracks == nil {
		return nil, fmt.Errorf("NewNowPlayingReader: the track source: %w", ErrNowPlayingCollaboratorRequired)
	}
	return &NowPlayingReader{tracks: tracks}, nil
}

func MustNewNowPlayingReader(tracks ports.TrackSource) *NowPlayingReader {
	r, err := NewNowPlayingReader(tracks)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *NowPlayingReader) Lookup(
	ctx context.Context,
	userId ids.UserId,
	trackId string,
) (*ports.NowPlayingTrack, error) {
	id, err := ids.ParseTrackId(trackId)
	if err != nil {
		slog.WarnContext(ctx, "now_playing.malformed_track_id",
			"raw_len", len(trackId), logging.KeyError, err)
		return nil, domain.ErrNowPlayingTrackNotFound
	}

	track, err := r.tracks.NowPlayingTrack(ctx, userId, id)
	switch {
	case errors.Is(err, domain.ErrNowPlayingTrackNotFound):
		return nil, domain.ErrNowPlayingTrackNotFound
	case errors.Is(err, domain.ErrEnrichmentUnavailable):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	return track, nil
}
