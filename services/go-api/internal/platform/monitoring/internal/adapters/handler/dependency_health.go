package handler

import (
	"altune/go-api/internal/shared/httpserver"
	"context"
	"net/http"
	"runtime"
	"time"
)

type DepStatus string

const (
	DepUp            DepStatus = "ok"
	DepNotConfigured DepStatus = "not_configured"
	DepDown          DepStatus = "down"
)

func (d DependencyHealth) Healthy() bool {
	return d.DB != DepDown && d.Redis != DepDown && d.Auth != DepDown
}

const (
	defaultProbeTimeout = 5 * time.Second
	bytesPerMB          = 1024 * 1024
)

type HealthProbe func(ctx context.Context) DependencyHealth

func (h *Handler) serveHealth(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	dependencies := h.runProbe(r.Context())
	httpserver.WriteJSON(w, healthStatusCode(dependencies), healthResponse{
		DependencyHealth: dependencies,
		Goroutines:       runtime.NumGoroutine(),
		HeapMB:           ms.HeapAlloc / bytesPerMB,
	})
}

func healthStatusCode(d DependencyHealth) int {
	if d.Healthy() {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}

func (h *Handler) runProbe(ctx context.Context) DependencyHealth {
	probeCtx, cancel := context.WithTimeout(ctx, h.probeTimeout)
	defer cancel()
	return h.deps.Health(probeCtx)
}
