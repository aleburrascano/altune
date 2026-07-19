// Package catalogbridge adapts the catalog context's track repository to the
// playback context's NowPlayingReader port. It is a playback outbound adapter: it
// depends only on the catalog *port* (interface) and catalog domain types, never
// on catalog's adapters — the standard cross-context integration seam. Its one job
// is translating a catalog Track into playback's display snapshot.
package catalogbridge

import (
	"context"
	"fmt"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

// trackReader is the narrow read this bridge actually calls, out of the
// catalog TrackRepository adapter's full surface.
type trackReader interface {
	GetByID(ctx context.Context, id catalogDomain.TrackId, userId shared.UserId) (*catalogDomain.Track, error)
}

type NowPlayingReader struct {
	tracks trackReader
}

func NewNowPlayingReader(tracks trackReader) *NowPlayingReader {
	return &NowPlayingReader{tracks: tracks}
}

func (r *NowPlayingReader) Lookup(
	ctx context.Context,
	userId shared.UserId,
	trackId string,
) (*ports.NowPlayingTrack, error) {
	id, err := catalogDomain.ParseTrackId(trackId)
	if err != nil {
		// A malformed id can't identify a track — treat as absent, not an error,
		// so resume degrades to "no snapshot" instead of failing the whole call.
		return nil, nil
	}

	track, err := r.tracks.GetByID(ctx, id, userId)
	if err != nil {
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	if track == nil {
		return nil, nil
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
