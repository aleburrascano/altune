package app

import (
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"sync"
	"time"
)

const healthCacheTTL = 2 * time.Second

var buildCommit = "unknown"

type healthCache struct {
	mu        sync.Mutex
	result    DependencyHealth
	expiresAt time.Time
}

func (c *healthCache) get(probe func() DependencyHealth) DependencyHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expiresAt) {
		return c.result
	}
	c.result = probe()
	c.expiresAt = time.Now().Add(healthCacheTTL)
	return c.result
}

type DependencyHealth struct {
	DB     DepStatus
	Redis  DepStatus
	Auth   DepStatus
	Detail DependencyDetail
}

type DepStatus string

const (
	// DepUp means the dependency is configured and answered its probe.
	DepUp DepStatus = "ok"
	// DepNotConfigured means the dependency is not wired; it does not fail readiness.
	DepNotConfigured DepStatus = "not_configured"
	// DepDown means the dependency is configured but its probe failed.
	DepDown DepStatus = "down"
)

// DependencyDetail carries per-dependency latency and error information
// gathered during a health probe.
type DependencyDetail struct {
	DBLatencyMs    int64
	DBError        string
	RedisLatencyMs int64
	RedisError     string
	AuthLatencyMs  int64
	AuthError      string
	CheckedAt      time.Time
}

// Healthy reports readiness: a dependency that is DepDown fails the check, while
// DepNotConfigured is treated as ready.
func (d DependencyHealth) Healthy() bool {
	return len(d.down()) == 0
}

func (d DependencyHealth) down() []string {
	var names []string
	for _, dep := range []struct {
		name   string
		status DepStatus
	}{{"db", d.DB}, {"redis", d.Redis}, {"auth", d.Auth}} {
		if dep.status == DepDown {
			names = append(names, dep.name)
		}
	}
	return names
}

func probeDependency(configured bool, run func() error) (DepStatus, string, int64) {
	if !configured {
		return DepNotConfigured, "", 0
	}
	start := time.Now()
	err := run()
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return DepDown, err.Error(), ms
	}
	return DepUp, "", ms
}

// authHealthChecker reports whether the auth subsystem can obtain its JWKS key
// set. *authProviders.SupabaseJWTVerifier satisfies it.
type authHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

// dbHealthChecker probes database reachability and returns the live
// HealthStatus, so the handler can surface the real error rather than a
// placeholder. It is a seam: production wires it to database.CheckHealth over
// the pool, and tests inject a stub.
type dbHealthChecker func(ctx context.Context) database.HealthStatus

// defaultDependencyProbeTimeout bounds each individual DB/Redis/auth call made
// by dependencyHealth. The plain, unauthenticated /health route passes the bare
// request context (no deadline), so without this a stalled dependency would
// hang the probe — and the endpoint — indefinitely.
const defaultDependencyProbeTimeout = 2 * time.Second

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := a.healthCache.get(func() DependencyHealth { return a.dependencyHealth(context.WithoutCancel(r.Context())) })
	if health.Healthy() {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildCommit})
		return
	}
	httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "version": buildCommit})
}

func (a *App) dependencyHealth(ctx context.Context) DependencyHealth {
	timeout := a.probeTimeout()
	dbStatus, dbErr, dbMs := probeDependency(a.dbHealth != nil, func() error {
		if status := a.probeDB(ctx, timeout); !status.OK {
			return status.Err
		}
		return nil
	})
	redisStatus, redisErr, redisMs := probeDependency(a.redisClient != nil, func() error {
		return probe(ctx, timeout, func(c context.Context) error {
			return a.redisClient.Ping(c).Err()
		})
	})
	authStatus, authErr, authMs := probeDependency(a.authVerifier != nil, func() error {
		return probe(ctx, timeout, a.authVerifier.CheckHealth)
	})
	return DependencyHealth{DB: dbStatus, Redis: redisStatus, Auth: authStatus, Detail: DependencyDetail{
		DBLatencyMs: dbMs, DBError: dbErr, RedisLatencyMs: redisMs, RedisError: redisErr,
		AuthLatencyMs: authMs, AuthError: authErr, CheckedAt: time.Now().UTC(),
	}}
}

// probeTimeout is the per-dependency bound, falling back to the package default
// when unset.
func (a *App) probeTimeout() time.Duration {
	if a.depProbeTimeout > 0 {
		return a.depProbeTimeout
	}
	return defaultDependencyProbeTimeout
}

// probeDB runs the DB health check under a bounded context derived from ctx.
func (a *App) probeDB(ctx context.Context, timeout time.Duration) database.HealthStatus {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return a.dbHealth(ctx)
}

// probe runs an error-returning dependency check under a bounded context
// derived from ctx, so a stalled call cannot hang the health probe.
func probe(ctx context.Context, timeout time.Duration, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return check(ctx)
}
