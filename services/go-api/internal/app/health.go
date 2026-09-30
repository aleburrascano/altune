package app

import (
	observeHandler "altune/go-api/internal/observe/handler"
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
	result    observeHandler.DependencyHealth
	expiresAt time.Time
	now       func() time.Time
}

func (c *healthCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *healthCache) get(probe func() observeHandler.DependencyHealth) observeHandler.DependencyHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.clock().Before(c.expiresAt) {
		return c.result
	}
	c.result = probe()
	c.expiresAt = c.clock().Add(healthCacheTTL)
	return c.result
}

type probeResult struct {
	status    observeHandler.DepStatus
	err       string
	latencyMs int64
}

func probeDependency(configured bool, run func() error) probeResult {
	if !configured {
		return probeResult{status: observeHandler.DepNotConfigured}
	}
	start := time.Now()
	err := run()
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return probeResult{status: observeHandler.DepDown, err: err.Error(), latencyMs: ms}
	}
	return probeResult{status: observeHandler.DepUp, latencyMs: ms}
}

type authHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

type dbHealthChecker func(ctx context.Context) database.HealthStatus

const defaultDependencyProbeTimeout = 2 * time.Second

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := a.healthCache.get(func() observeHandler.DependencyHealth { return a.dependencyHealth(context.WithoutCancel(r.Context())) })
	if health.Healthy() {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": buildCommit})
		return
	}
	httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "version": buildCommit})
}

func (a *App) dependencyHealth(ctx context.Context) observeHandler.DependencyHealth {
	timeout := a.probeTimeout()
	db := probeDependency(a.dbHealth != nil, func() error {
		if status := a.probeDB(ctx, timeout); !status.OK {
			return status.Err
		}
		return nil
	})
	redis := probeDependency(a.redisClient != nil, func() error {
		return probe(ctx, timeout, func(c context.Context) error {
			return a.redisClient.Ping(c).Err()
		})
	})
	auth := probeDependency(a.authVerifier != nil, func() error {
		return probe(ctx, timeout, a.authVerifier.CheckHealth)
	})
	return observeHandler.DependencyHealth{DB: db.status, Redis: redis.status, Auth: auth.status, Detail: observeHandler.DependencyDetail{
		DBLatencyMs: db.latencyMs, DBError: db.err, RedisLatencyMs: redis.latencyMs, RedisError: redis.err,
		AuthLatencyMs: auth.latencyMs, AuthError: auth.err, CheckedAt: time.Now().UTC(),
	}}
}

func (a *App) probeTimeout() time.Duration {
	if a.depProbeTimeout > 0 {
		return a.depProbeTimeout
	}
	return defaultDependencyProbeTimeout
}

func (a *App) probeDB(ctx context.Context, timeout time.Duration) database.HealthStatus {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return a.dbHealth(ctx)
}

func probe(ctx context.Context, timeout time.Duration, check func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return check(ctx)
}
