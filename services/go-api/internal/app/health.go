package app

import (
	"context"
	"net/http"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/httputil"
)

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
	if a.pool == nil {
		dbStatus = "not_configured"
	} else {
		start := time.Now()
		if !database.CheckHealth(ctx, a.pool).OK {
			dbStatus = "down"
			detail.DBError = "health check failed"
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

	return adminHandler.DependencyHealth{DB: dbStatus, Redis: redisStatus, Detail: detail}
}
