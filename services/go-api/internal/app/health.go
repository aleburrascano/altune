package app

import (
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
)

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
	if a.dependencyHealth(r.Context()).Healthy() {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
}

func (a *App) dependencyHealth(ctx context.Context) adminHandler.DependencyHealth {
	detail := adminHandler.DependencyDetail{CheckedAt: time.Now().UTC()}
	timeout := a.probeTimeout()

	dbStatus := "ok"
	if a.dbHealth == nil {
		dbStatus = "not_configured"
	} else {
		start := time.Now()
		status := a.probeDB(ctx, timeout)
		if !status.OK {
			dbStatus = "down"
			detail.DBError = status.Err.Error()
		}
		detail.DBLatencyMs = time.Since(start).Milliseconds()
	}

	redisStatus := "ok"
	if a.redisClient == nil {
		redisStatus = "not_configured"
	} else {
		start := time.Now()
		if err := probe(ctx, timeout, func(c context.Context) error {
			return a.redisClient.Ping(c).Err()
		}); err != nil {
			redisStatus = "down"
			detail.RedisError = err.Error()
		}
		detail.RedisLatencyMs = time.Since(start).Milliseconds()
	}

	authStatus := "ok"
	if a.authVerifier == nil {
		authStatus = "not_configured"
	} else {
		start := time.Now()
		if err := probe(ctx, timeout, a.authVerifier.CheckHealth); err != nil {
			authStatus = "down"
			detail.AuthError = err.Error()
		}
		detail.AuthLatencyMs = time.Since(start).Milliseconds()
	}

	return adminHandler.DependencyHealth{DB: dbStatus, Redis: redisStatus, Auth: authStatus, Detail: detail}
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
