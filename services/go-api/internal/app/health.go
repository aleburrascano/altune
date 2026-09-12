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

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	if a.dependencyHealth(r.Context()).Healthy() {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	httputil.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded"})
}

func (a *App) dependencyHealth(ctx context.Context) adminHandler.DependencyHealth {
	detail := adminHandler.DependencyDetail{CheckedAt: time.Now().UTC()}

	dbStatus := "ok"
	if a.dbHealth == nil {
		dbStatus = "not_configured"
	} else {
		start := time.Now()
		if status := a.dbHealth(ctx); !status.OK {
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
		if err := a.redisClient.Ping(ctx).Err(); err != nil {
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
		if err := a.authVerifier.CheckHealth(ctx); err != nil {
			authStatus = "down"
			detail.AuthError = err.Error()
		}
		detail.AuthLatencyMs = time.Since(start).Milliseconds()
	}

	return adminHandler.DependencyHealth{DB: dbStatus, Redis: redisStatus, Auth: authStatus, Detail: detail}
}
