package handler

import (
	"altune/go-api/internal/shared/httputil"
	"context"
	"net/http"
	"runtime"
	"time"
)

// DepStatus is the closed tri-state a dependency probe reports. Its string
// values are the wire contract of /admin/health.
type DepStatus string

const (
	// DepUp means the dependency is configured and answered its probe.
	DepUp DepStatus = "ok"
	// DepNotConfigured means the dependency is not wired; it does not fail readiness.
	DepNotConfigured DepStatus = "not_configured"
	// DepDown means the dependency is configured but its probe failed.
	DepDown DepStatus = "down"
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

// defaultProbeTimeout bounds the dependency probe so a stalled DB/Redis cannot
// park an /admin/health request forever.
const defaultProbeTimeout = 5 * time.Second

type HealthProbe func(ctx context.Context) DependencyHealth

func (h *AdminHandler) serveHealth(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	dependencies := h.runProbe(r.Context())
	httputil.WriteJSON(w, healthStatusCode(dependencies), healthResponse{
		DependencyHealth: dependencies,
		Goroutines:       runtime.NumGoroutine(),
		HeapMB:           ms.HeapAlloc / (1024 * 1024),
	})
}

// healthStatusCode lets a status-code monitor read the operator route's verdict
// without decoding the body, matching the public /health route. The body is the
// same either way: 503 is the incident an operator reads the detail during.
func healthStatusCode(d DependencyHealth) int {
	if d.Healthy() {
		return http.StatusOK
	}
	return http.StatusServiceUnavailable
}

// runProbe invokes the injected probe under a bounded timeout derived from the
// request context, so a stuck dependency surfaces as a deadline instead of
// hanging the request.
func (h *AdminHandler) runProbe(ctx context.Context) DependencyHealth {
	timeout := h.probeTimeout
	if timeout <= 0 {
		timeout = defaultProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return h.probe(probeCtx)
}
