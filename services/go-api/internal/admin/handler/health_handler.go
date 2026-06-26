package handler

import (
	"context"
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

// DependencyHealth is the per-dependency status shown on the operator health
// tiles. Each value is one of "ok", "down", or "not_configured".
type DependencyHealth struct {
	DB    string `json:"db"`
	Redis string `json:"redis"`
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
	httputil.WriteJSON(w, http.StatusOK, h.probe(r.Context()))
}
