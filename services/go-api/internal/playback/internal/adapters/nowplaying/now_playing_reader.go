package nowplaying

import (
	"altune/go-api/internal/playback/internal/core/ports"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const defaultNowPlayingLookupTimeout = 3 * time.Second

var errEnrichmentUnavailable = errors.New("now-playing enrichment temporarily unavailable")

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type NowPlayingReader struct {
	tracks  ports.TrackSource
	breaker *enrichmentBreaker
	metrics ports.EnrichmentMetrics
	timeout time.Duration
	now     func() time.Time
}

func NewNowPlayingReader(tracks ports.TrackSource, opts ...func(*NowPlayingReader)) *NowPlayingReader {
	r := &NowPlayingReader{tracks: tracks, metrics: ports.NoopEnrichmentMetrics(), timeout: defaultNowPlayingLookupTimeout, now: time.Now}
	for _, opt := range opts {
		opt(r)
	}
	r.breaker = newEnrichmentBreaker(r.metrics, r.now)
	return r
}

func WithNowPlayingMetrics(m ports.EnrichmentMetrics) func(*NowPlayingReader) {
	return func(r *NowPlayingReader) {
		if m != nil {
			r.metrics = m
		}
	}
}

func WithNowPlayingLookupTimeout(d time.Duration) func(*NowPlayingReader) {
	return func(r *NowPlayingReader) {
		if d > 0 {
			r.timeout = d
		}
	}
}

func WithNowPlayingClock(now func() time.Time) func(*NowPlayingReader) {
	return func(r *NowPlayingReader) {
		if now != nil {
			r.now = now
		}
	}
}

func isDependencyFailure(err error) bool {
	return errors.Is(err, ports.ErrTrackSourceUnavailable) || errors.Is(err, context.DeadlineExceeded)
}

func (r *NowPlayingReader) Lookup(
	ctx context.Context,
	userId shared.UserId,
	trackId string,
) (*ports.NowPlayingTrack, error) {
	id, err := shared.ParseTrackId(trackId)
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
