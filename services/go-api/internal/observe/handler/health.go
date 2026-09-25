package handler

import (
	"altune/go-api/internal/shared/httputil"
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

type DependencyHealth struct {
	DB     DepStatus        `json:"db"`
	Redis  DepStatus        `json:"redis"`
	Auth   DepStatus        `json:"auth"`
	Detail DependencyDetail `json:"detail"`
}

type DependencyDetail struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	AuthLatencyMs  int64     `json:"auth_latency_ms"`
	AuthError      string    `json:"auth_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

type healthResponse struct {
	DependencyHealth
	Goroutines int    `json:"goroutines"`
	HeapMB     uint64 `json:"heap_mb"`
}

func (d DependencyHealth) Healthy() bool {
	return d.DB != DepDown && d.Redis != DepDown && d.Auth != DepDown
}

const defaultProbeTimeout = 5 * time.Second

type HealthProbe func(ctx context.Context) DependencyHealth

func (h *Handler) serveHealth(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	dependencies := h.runProbe(r.Context())
	httputil.WriteJSON(w, healthStatusCode(dependencies), healthResponse{
		DependencyHealth: dependencies,
		Goroutines:       runtime.NumGoroutine(),
		HeapMB:           ms.HeapAlloc / (1024 * 1024),
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
