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

func NewNowPlayingReader(tracks ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) *NowPlayingReader {
	if tracks == nil {
		panic("nowplaying: NewNowPlayingReader requires a track source")
	}
	if metrics == nil {
		panic("nowplaying: NewNowPlayingReader requires enrichment metrics")
	}
	if now == nil {
		panic("nowplaying: NewNowPlayingReader requires a clock")
	}
	if lookupTimeout <= 0 {
		panic("nowplaying: NewNowPlayingReader requires a positive lookup timeout")
	}
	return &NowPlayingReader{tracks: tracks, breaker: newEnrichmentBreaker(metrics, now), metrics: metrics, timeout: lookupTimeout}
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
				r.breaker.recordFailure(ctx)
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
