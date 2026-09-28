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
	DepUp            DepStatus = "ok"
	DepNotConfigured DepStatus = "not_configured"
	DepDown          DepStatus = "down"
)

type DependencyDetail struct {
	DBLatencyMs    int64
	DBError        string
	RedisLatencyMs int64
	RedisError     string
	AuthLatencyMs  int64
	AuthError      string
	CheckedAt      time.Time
}

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

type authHealthChecker interface {
	CheckHealth(ctx context.Context) error
}

type dbHealthChecker func(ctx context.Context) database.HealthStatus

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
