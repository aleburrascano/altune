package nowplaying

import (
	"altune/go-api/internal/playback/internal/core/ports"
	"altune/go-api/internal/shared/breaker"
	"context"
	"log/slog"
	"time"
)

const (
	enrichmentFailureThreshold = 5
	enrichmentOpenDuration     = 30 * time.Second
	enrichmentProbeLease       = 30 * time.Second
)

type enrichmentBreaker struct {
	b *breaker.Breaker
}

func newEnrichmentBreaker(metrics ports.EnrichmentMetrics, now func() time.Time) *enrichmentBreaker {
	return &enrichmentBreaker{b: breaker.New(breaker.Config{
		Threshold:  enrichmentFailureThreshold,
		OpenFor:    enrichmentOpenDuration,
		ProbeLease: enrichmentProbeLease,
		Now:        now,
		OnChange:   observeEnrichmentBreaker(metrics),
	})}
}

func observeEnrichmentBreaker(metrics ports.EnrichmentMetrics) func(context.Context, breaker.Change) {
	return func(ctx context.Context, c breaker.Change) {
		switch c.Kind {
		case breaker.Tripped:
			metrics.EnrichmentBreakerOpened()
			slog.WarnContext(ctx, "now-playing enrichment breaker opened (catalog failing)", "failures", c.Failures)
		case breaker.Probing:
			slog.WarnContext(ctx, "now-playing enrichment breaker half-open (probing catalog recovery)")
		case breaker.LeaseExpired:
			slog.WarnContext(ctx, "now-playing enrichment breaker probe lease expired (re-probing)")
		case breaker.Recovered:
			metrics.EnrichmentBreakerClosed()
			slog.InfoContext(ctx, "now-playing enrichment breaker closed (catalog recovered)")
		}
	}
}

func (e *enrichmentBreaker) allow(ctx context.Context) (bool, breaker.Admission) {
	adm, ok := e.b.Allow(ctx)
	return ok, adm
}

func (e *enrichmentBreaker) release(adm breaker.Admission) {
	e.b.Release(adm)
}

func (e *enrichmentBreaker) recordSuccess(ctx context.Context, adm breaker.Admission) {
	e.b.Success(ctx, adm)
}

func (e *enrichmentBreaker) recordFailure(ctx context.Context, adm breaker.Admission) {
	e.b.Failure(ctx, adm)
}
