package middleware

import (
	"altune/go-api/internal/shared/clock"
	"cmp"
	"slices"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type FailureLimits struct {
	Burst      int
	Refill     time.Duration
	MaxClients int
}

var DefaultFailureLimits = FailureLimits{
	Burst:      20,
	Refill:     3 * time.Second,
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
	clients map[string]*rate.Limiter
}

func newFailureThrottle(limits FailureLimits, now clock.Func) *failureThrottle {
	return &failureThrottle{limits: limits, now: now, clients: make(map[string]*rate.Limiter)}
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
	res := t.limiterFor(key, now).ReserveN(now, 1)
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

func (t *failureThrottle) limiterFor(key string, now time.Time) *rate.Limiter {
	if lim, ok := t.clients[key]; ok {
		return lim
	}
	t.makeRoom(now)
	lim := rate.NewLimiter(rate.Every(t.limits.Refill), t.limits.Burst)
	t.clients[key] = lim
	return lim
}

func (t *failureThrottle) makeRoom(now time.Time) {
	if len(t.clients) < t.limits.MaxClients {
		return
	}
	t.pruneRefilled(now)
	t.evictFullest(now, t.limits.MaxClients-max(1, t.limits.MaxClients/10))
}

func (t *failureThrottle) pruneRefilled(now time.Time) {
	for key, lim := range t.clients {
		if lim.TokensAt(now) >= float64(t.limits.Burst) {
			delete(t.clients, key)
		}
	}
}

func (t *failureThrottle) evictFullest(now time.Time, lowWater int) {
	byTokens := make(map[string]float64, len(t.clients))
	keys := make([]string, 0, len(t.clients))
	for key, lim := range t.clients {
		byTokens[key] = lim.TokensAt(now)
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int { return cmp.Compare(byTokens[b], byTokens[a]) })
	for _, key := range keys {
		if len(t.clients) <= lowWater {
			return
		}
		delete(t.clients, key)
	}
}
