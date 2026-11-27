package middleware

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/expiringcache"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type FailureLimits struct {
	Burst      int
	Refill     time.Duration
	MaxClients int
}

const failureRefillInterval = 3 * time.Second

var DefaultFailureLimits = FailureLimits{
	Burst:      20,
	Refill:     failureRefillInterval,
	MaxClients: 10_000,
}

func (l FailureLimits) orDefaults() FailureLimits {
	return FailureLimits{
		Burst:      positiveOr(l.Burst, DefaultFailureLimits.Burst),
		Refill:     positiveOr(l.Refill, DefaultFailureLimits.Refill),
		MaxClients: positiveOr(l.MaxClients, DefaultFailureLimits.MaxClients),
	}
}

func positiveOr[T int | time.Duration](v, fallback T) T {
	if v > 0 {
		return v
	}
	return fallback
}

type failureThrottle struct {
	mu      sync.Mutex
	limits  FailureLimits
	now     clock.Func
	clients *expiringcache.Keyed[string, *rate.Limiter]
}

func newFailureThrottle(limits FailureLimits, now clock.Func) *failureThrottle {
	return &failureThrottle{limits: limits, now: now, clients: expiringcache.NewKeyed[string, *rate.Limiter](limits.Refill*time.Duration(limits.Burst), limits.MaxClients, now)}
}

type attempt struct {
	throttle    *failureThrottle
	reservation *rate.Reservation
	reservedAt  time.Time
}

func (t *failureThrottle) admit(key string) (attempt, time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	res := t.limiterFor(key).ReserveN(now, 1)
	if delay := res.DelayFrom(now); delay > 0 {
		res.CancelAt(now)
		return attempt{}, delay, false
	}
	return attempt{throttle: t, reservation: res, reservedAt: now}, 0, true
}

func (a attempt) succeeded() {
	a.throttle.cancel(a.reservation, a.reservedAt)
}

func (t *failureThrottle) cancel(res *rate.Reservation, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	res.CancelAt(at)
}

func (t *failureThrottle) limiterFor(key string) *rate.Limiter {
	return t.clients.Get(key, func() *rate.Limiter {
		return rate.NewLimiter(rate.Every(t.limits.Refill), t.limits.Burst)
	})
}
