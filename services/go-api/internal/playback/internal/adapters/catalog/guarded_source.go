package catalog

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/shared/breaker"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"time"
)

var _ ports.TrackSource = (*GuardedSource)(nil)

var (
	ErrCollaboratorRequired = errors.New("nowplaying: a required collaborator is missing")
	ErrClockRequired        = errors.New("nowplaying: the reader requires a clock")
	ErrInvalidTimeout       = errors.New("nowplaying: the lookup timeout must be positive")
)

type GuardedSource struct {
	next    ports.TrackSource
	breaker *enrichmentBreaker
	metrics ports.EnrichmentMetrics
	timeout time.Duration
}

func NewGuardedSource(next ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) (*GuardedSource, error) {
	switch {
	case next == nil:
		return nil, fmt.Errorf("NewGuardedSource: the track source: %w", ErrCollaboratorRequired)
	case metrics == nil:
		return nil, fmt.Errorf("NewGuardedSource: the enrichment metrics: %w", ErrCollaboratorRequired)
	case now == nil:
		return nil, fmt.Errorf("NewGuardedSource: %w", ErrClockRequired)
	case lookupTimeout <= 0:
		return nil, fmt.Errorf("NewGuardedSource: %w", ErrInvalidTimeout)
	}
	return &GuardedSource{next: next, breaker: newEnrichmentBreaker(metrics, now), metrics: metrics, timeout: lookupTimeout}, nil
}

func MustNewGuardedSource(next ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) *GuardedSource {
	g, err := NewGuardedSource(next, metrics, now, lookupTimeout)
	if err != nil {
		panic(err)
	}
	return g
}

func (g *GuardedSource) NowPlayingTrack(ctx context.Context, userId ids.UserId, id ids.TrackId) (*ports.NowPlayingTrack, error) {
	admitted, admission := g.breaker.allow(ctx)
	if !admitted {
		g.metrics.EnrichmentBreakerRejected()
		return nil, domain.ErrEnrichmentUnavailable
	}
	defer g.breaker.release(admission)

	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	track, err := g.next.NowPlayingTrack(callCtx, userId, id)
	if errors.Is(err, domain.ErrNowPlayingTrackNotFound) {
		g.breaker.recordSuccess(ctx, admission)
		return nil, domain.ErrNowPlayingTrackNotFound
	}
	if err != nil {
		g.recordLookupFailure(ctx, admission, err)
		return nil, err
	}
	g.breaker.recordSuccess(ctx, admission)
	return track, nil
}

func (g *GuardedSource) recordLookupFailure(ctx context.Context, admission breaker.Admission, err error) {
	if ctx.Err() != nil {
		return
	}
	if isDependencyFailure(err) {
		g.breaker.recordFailure(ctx, admission)
	}
	g.metrics.EnrichmentFailed()
	if errors.Is(err, context.DeadlineExceeded) {
		g.metrics.NowPlayingLookupTimedOut()
	}
}

func isDependencyFailure(err error) bool {
	return errors.Is(err, domain.ErrTrackSourceUnavailable) || errors.Is(err, context.DeadlineExceeded)
}
