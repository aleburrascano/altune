package handler

import (
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"runtime"
	"time"
)

type DependencyHealth struct {
	DB     string           `json:"db"`
	Redis  string           `json:"redis"`
	Auth   string           `json:"auth"`
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
	return d.DB != statusDown && d.Redis != statusDown && d.Auth != statusDown
}

const statusDown = "down"

type HealthProbe func(ctx context.Context) DependencyHealth

func (h *AdminHandler) serveHealth(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	httputil.WriteJSON(w, http.StatusOK, healthResponse{
		DependencyHealth: h.probe(r.Context()),
		Goroutines:       runtime.NumGoroutine(),
		HeapMB:           ms.HeapAlloc / (1024 * 1024),
	})
}
