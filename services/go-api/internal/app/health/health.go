package health

import (
	"altune/go-api/internal/observe"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/runloop"
	"context"
	"net/http"
	"sync"
	"time"
)

const (
	DefaultProbeTimeout = 2 * time.Second
	cacheTTL            = 2 * time.Second
	checkBudget         = 3 * DefaultProbeTimeout
)

type Probes struct {
	DB    func(context.Context) error
	Redis func(context.Context) error
	Auth  func(context.Context) error
}

type probeResult struct {
	status    observe.DepStatus
	err       string
	latencyMs int64
}

func Check(ctx context.Context, probes Probes, timeout time.Duration) observe.DependencyHealth {
	db := probe(ctx, probes.DB, timeout)
	redis := probe(ctx, probes.Redis, timeout)
	auth := probe(ctx, probes.Auth, timeout)
	return observe.DependencyHealth{DB: db.status, Redis: redis.status, Auth: auth.status, Detail: observe.DependencyDetail{
		DBLatencyMs: db.latencyMs, DBError: db.err, RedisLatencyMs: redis.latencyMs, RedisError: redis.err,
		AuthLatencyMs: auth.latencyMs, AuthError: auth.err, CheckedAt: time.Now().UTC(),
	}}
}

func probe(ctx context.Context, check func(context.Context) error, timeout time.Duration) probeResult {
	if check == nil {
		return probeResult{status: observe.DepNotConfigured}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	err := check(ctx)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return probeResult{status: observe.DepDown, err: err.Error(), latencyMs: ms}
	}
	return probeResult{status: observe.DepUp, latencyMs: ms}
}

type Endpoint struct {
	check   func(context.Context) observe.DependencyHealth
	version string
	now     func() time.Time

	mu        sync.Mutex
	result    observe.DependencyHealth
	expiresAt time.Time
}

func NewEndpoint(check func(context.Context) observe.DependencyHealth, version string, now func() time.Time) *Endpoint {
	return &Endpoint{check: check, version: version, now: now}
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := runloop.Detach(r.Context(), checkBudget)
	defer cancel()
	if e.cached(ctx).DB == observe.DepDown {
		httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "version": e.version})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": e.version})
}

func (e *Endpoint) cached(ctx context.Context) observe.DependencyHealth {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.now().Before(e.expiresAt) {
		return e.result
	}
	e.result = e.check(ctx)
	e.expiresAt = e.now().Add(cacheTTL)
	return e.result
}
