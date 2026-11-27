package service

import (
	ports "altune/go-api/internal/playback/internal"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/shared/breaker"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
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
	ErrNowPlayingCollaboratorRequired = errors.New("nowplaying: a required collaborator is missing")
	ErrNowPlayingClockRequired        = errors.New("nowplaying: the reader requires a clock")
	ErrNowPlayingInvalidTimeout       = errors.New("nowplaying: the lookup timeout must be positive")
)

func NewNowPlayingReader(tracks ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) (*NowPlayingReader, error) {
	switch {
	case tracks == nil:
		return nil, fmt.Errorf("NewNowPlayingReader: the track source: %w", ErrNowPlayingCollaboratorRequired)
	case metrics == nil:
		return nil, fmt.Errorf("NewNowPlayingReader: the enrichment metrics: %w", ErrNowPlayingCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewNowPlayingReader: %w", ErrNowPlayingClockRequired)
	case lookupTimeout <= 0:
		return nil, fmt.Errorf("NewNowPlayingReader: %w", ErrNowPlayingInvalidTimeout)
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
	return errors.Is(err, domain.ErrTrackSourceUnavailable) || errors.Is(err, context.DeadlineExceeded)
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

	admitted, admission := r.breaker.allow(ctx)
	if !admitted {
		r.metrics.EnrichmentBreakerRejected()
		return nil, errEnrichmentUnavailable
	}
	defer r.breaker.release(admission)

	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	track, err := r.tracks.NowPlayingTrack(callCtx, userId, id)
	if errors.Is(err, domain.ErrNowPlayingTrackNotFound) {
		r.breaker.recordSuccess(ctx, admission)
		return nil, domain.ErrNowPlayingTrackNotFound
	}
	if err != nil {
		r.recordLookupFailure(ctx, admission, err)
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	r.breaker.recordSuccess(ctx, admission)
	return track, nil
}

func (r *NowPlayingReader) recordLookupFailure(ctx context.Context, admission breaker.Admission, err error) {
	if ctx.Err() != nil {
		return
	}
	if isDependencyFailure(err) {
		r.breaker.recordFailure(ctx, admission)
	}
	r.metrics.EnrichmentFailed()
	if errors.Is(err, context.DeadlineExceeded) {
		r.metrics.NowPlayingLookupTimedOut()
	}
}
