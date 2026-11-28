package catalog

import (
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/resilience"
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
	b *resilience.CircuitBreaker
}

func newEnrichmentBreaker(metrics ports.EnrichmentMetrics, now clock.Func) *enrichmentBreaker {
	return &enrichmentBreaker{b: resilience.MustNewCircuitBreaker(resilience.CircuitBreakerConfig{
		Threshold:  enrichmentFailureThreshold,
		OpenFor:    enrichmentOpenDuration,
		ProbeLease: enrichmentProbeLease,
		Now:        now,
		OnChange:   observeEnrichmentBreaker(metrics),
	})}
}

func observeEnrichmentBreaker(metrics ports.EnrichmentMetrics) func(context.Context, resilience.CircuitChange) {
	return func(ctx context.Context, c resilience.CircuitChange) {
		switch c.Kind {
		case resilience.CircuitTripped:
			metrics.EnrichmentBreakerOpened()
			slog.WarnContext(ctx, "now-playing enrichment breaker opened (catalog failing)", "failures", c.Failures)
		case resilience.CircuitProbing:
			slog.WarnContext(ctx, "now-playing enrichment breaker half-open (probing catalog recovery)")
		case resilience.ProbeLeaseExpired:
			slog.WarnContext(ctx, "now-playing enrichment breaker probe lease expired (re-probing)")
		case resilience.CircuitRecovered:
			metrics.EnrichmentBreakerClosed()
			slog.InfoContext(ctx, "now-playing enrichment breaker closed (catalog recovered)")
		}
	}
}

func (e *enrichmentBreaker) allow(ctx context.Context) (bool, resilience.CircuitAdmission) {
	adm, ok := e.b.Allow(ctx)
	return ok, adm
}

func (e *enrichmentBreaker) release(adm resilience.CircuitAdmission) {
	e.b.Release(adm)
}

func (e *enrichmentBreaker) recordSuccess(ctx context.Context, adm resilience.CircuitAdmission) {
	e.b.Success(ctx, adm)
}

func (e *enrichmentBreaker) recordFailure(ctx context.Context, adm resilience.CircuitAdmission) {
	e.b.Failure(ctx, adm)
}
