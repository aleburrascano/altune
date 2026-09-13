package catalogbridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
)

var nowPlayingLookupTimeout = 3 * time.Second

// errEnrichmentUnavailable is returned when the fast-fail breaker is open, so
// resume degrades instantly instead of stalling for the full per-call timeout.
var errEnrichmentUnavailable = errors.New("now-playing enrichment temporarily unavailable")

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type trackReader interface {
	GetByID(ctx context.Context, id catalogDomain.TrackId, userId shared.UserId) (*catalogDomain.Track, error)
}

type NowPlayingReader struct {
	tracks  trackReader
	breaker *enrichmentBreaker
}

func NewNowPlayingReader(tracks trackReader) *NowPlayingReader {
	return &NowPlayingReader{tracks: tracks, breaker: newEnrichmentBreaker()}
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

	if !r.breaker.allow() {
		return nil, errEnrichmentUnavailable
	}

	callCtx, cancel := context.WithTimeout(ctx, nowPlayingLookupTimeout)
	defer cancel()

	track, err := r.tracks.GetByID(callCtx, id, userId)
	if err != nil {
		// A caller-side cancellation is the client disconnecting, not a
		// catalog outage, so it must not trip the breaker against a healthy
		// catalog. Only failures owned by the dependency count.
		if ctx.Err() == nil {
			r.breaker.recordFailure()
		}
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	r.breaker.recordSuccess()
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
