package handler

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"altune/go-api/internal/shared/httputil"
)

// DependencyHealth is the per-dependency status shown on the operator health
// tiles. Each top-level value is one of "ok", "down", or "not_configured";
// Detail carries the latency/error breakdown behind each tile.
type DependencyHealth struct {
	DB     string           `json:"db"`
	Redis  string           `json:"redis"`
	Detail DependencyDetail `json:"detail"`
}

// DependencyDetail is the per-dependency latency + error surfaced when an
// operator opens a health tile.
type DependencyDetail struct {
	DBLatencyMs    int64     `json:"db_latency_ms"`
	DBError        string    `json:"db_error,omitempty"`
	RedisLatencyMs int64     `json:"redis_latency_ms"`
	RedisError     string    `json:"redis_error,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

// healthResponse augments the dependency health with process-level stats for the
// operator console's Health tab.
type healthResponse struct {
	DependencyHealth
	Goroutines int    `json:"goroutines"`
	HeapMB     uint64 `json:"heap_mb"`
}

// Healthy reports whether every *configured* dependency is up. A
// not_configured dependency is intentionally absent, not a failure, so it does
// not make the service unready.
func (d DependencyHealth) Healthy() bool {
	return d.DB != statusDown && d.Redis != statusDown
}

const statusDown = "down"

// HealthProbe returns the current per-dependency health. The composition root
// supplies the concrete probe (it closes over the DB pool and Redis client);
// the handler stays unaware of those dependencies.
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
