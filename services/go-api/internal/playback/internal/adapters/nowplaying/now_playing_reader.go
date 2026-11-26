package nowplaying

import (
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

var errEnrichmentUnavailable = errors.New("now-playing enrichment temporarily unavailable")

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type NowPlayingReader struct {
	tracks  ports.TrackSource
	breaker *enrichmentBreaker
	metrics ports.EnrichmentMetrics
	timeout time.Duration
}

var (
	ErrCollaboratorRequired = errors.New("nowplaying: a required collaborator is missing")
	ErrClockRequired        = errors.New("nowplaying: the reader requires a clock")
	ErrInvalidTimeout       = errors.New("nowplaying: the lookup timeout must be positive")
)

func NewNowPlayingReader(tracks ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) (*NowPlayingReader, error) {
	switch {
	case tracks == nil:
		return nil, fmt.Errorf("NewNowPlayingReader: the track source: %w", ErrCollaboratorRequired)
	case metrics == nil:
		return nil, fmt.Errorf("NewNowPlayingReader: the enrichment metrics: %w", ErrCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewNowPlayingReader: %w", ErrClockRequired)
	case lookupTimeout <= 0:
		return nil, fmt.Errorf("NewNowPlayingReader: %w", ErrInvalidTimeout)
	}
	return &NowPlayingReader{tracks: tracks, breaker: newEnrichmentBreaker(metrics, now), metrics: metrics, timeout: lookupTimeout}, nil
}

func MustNewNowPlayingReader(tracks ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) *NowPlayingReader {
	r, err := NewNowPlayingReader(tracks, metrics, now, lookupTimeout)
	if err != nil {
		panic(err)
	}
	return r
}

func isDependencyFailure(err error) bool {
	return errors.Is(err, ports.ErrTrackSourceUnavailable) || errors.Is(err, context.DeadlineExceeded)
}

func (r *NowPlayingReader) Lookup(
	ctx context.Context,
	userId ids.UserId,
	trackId string,
) (*ports.NowPlayingTrack, error) {
	id, err := ids.ParseTrackId(trackId)
	if err != nil {
		slog.WarnContext(ctx, "now_playing.malformed_track_id",
			"user_id", userId.String(), "raw_len", len(trackId), "error", err)
		return nil, ports.ErrNowPlayingTrackNotFound
	}

	admitted, admission := r.breaker.allow(ctx)
	if !admitted {
		r.metrics.EnrichmentBreakerRejected()
		return nil, errEnrichmentUnavailable
	}
	defer r.breaker.release(admission)

	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	track, err := r.tracks.NowPlayingTrack(callCtx, userId, id)
	if errors.Is(err, ports.ErrNowPlayingTrackNotFound) {
		r.breaker.recordSuccess(ctx, admission)
		return nil, ports.ErrNowPlayingTrackNotFound
	}
	if err != nil {
		if ctx.Err() == nil {
			if isDependencyFailure(err) {
				r.breaker.recordFailure(ctx, admission)
			}
			r.metrics.EnrichmentFailed()
			if errors.Is(err, context.DeadlineExceeded) {
				r.metrics.NowPlayingLookupTimedOut()
			}
		}
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	r.breaker.recordSuccess(ctx, admission)
	return track, nil
}
