package providerguard

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/shared/breaker"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

type (
	CircuitState = breaker.State
	Admission    = breaker.Admission
)

const (
	CircuitClosed   = breaker.Closed
	CircuitOpen     = breaker.Open
	CircuitHalfOpen = breaker.HalfOpen
)

const (
	FailureThreshold = 5
	OpenDuration     = 30 * time.Second
	probeLease       = 30 * time.Second
)

type CircuitBreaker struct {
	mu       sync.Mutex
	now      func() time.Time
	skew     atomic.Int64
	circuits map[domain.ProviderName]*breaker.Breaker
}

func NewCircuitBreaker() *CircuitBreaker {
	return newCircuitBreaker(time.Now)
}

func newCircuitBreaker(now func() time.Time) *CircuitBreaker {
	return &CircuitBreaker{
		now:      now,
		circuits: make(map[domain.ProviderName]*breaker.Breaker),
	}
}

func (cb *CircuitBreaker) AllowRequest(ctx context.Context, provider domain.ProviderName) (Admission, bool) {
	return cb.circuit(provider).Allow(ctx)
}

func (cb *CircuitBreaker) RecordSuccess(ctx context.Context, provider domain.ProviderName, adm Admission) {
	cb.circuit(provider).Success(ctx, adm)
}

func (cb *CircuitBreaker) RecordFailure(ctx context.Context, provider domain.ProviderName, adm Admission) {
	cb.circuit(provider).Failure(ctx, adm)
}

func (cb *CircuitBreaker) ReleaseProbe(provider domain.ProviderName, adm Admission) {
	cb.circuit(provider).Release(adm)
}

func (cb *CircuitBreaker) GetStatus(provider domain.ProviderName) domain.ProviderStatus {
	if cb.circuit(provider).State() == breaker.Open {
		return domain.ProviderStatusCircuitOpen
	}
	return domain.ProviderStatusOK
}

func (cb *CircuitBreaker) circuit(provider domain.ProviderName) *breaker.Breaker {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	b, ok := cb.circuits[provider]
	if !ok {
		b = breaker.New(breaker.Config{
			Threshold:  FailureThreshold,
			OpenFor:    OpenDuration,
			ProbeLease: probeLease,
			Now:        cb.clock,
			OnChange:   logChange(provider),
		})
		cb.circuits[provider] = b
	}
	return b
}

func (cb *CircuitBreaker) clock() time.Time {
	return cb.now().Add(time.Duration(cb.skew.Load()))
}

func logChange(provider domain.ProviderName) func(context.Context, breaker.Change) {
	return func(ctx context.Context, c breaker.Change) {
		switch c.Kind {
		case breaker.Tripped:
			slog.WarnContext(ctx, "circuit breaker opened (provider failing)", "provider", provider.String(), "failures", c.Failures)
		case breaker.Probing:
			slog.WarnContext(ctx, "circuit breaker half-open (probing recovery)", "provider", provider.String())
		case breaker.LeaseExpired:
			slog.WarnContext(ctx, "circuit breaker probe lease expired (re-probing)", "provider", provider.String())
		case breaker.Recovered:
			slog.InfoContext(ctx, "circuit breaker closed (provider recovered)", "provider", provider.String())
		}
	}
}
