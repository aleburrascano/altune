package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/service"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/httpserver"
	"context"
	"net/http"
	"sync"
	"time"
)

const (
	endpointCacheTTL = 2 * time.Second
	endpointBudget   = 3 * service.DefaultProbeTimeout
)

type Endpoint struct {
	check   func(context.Context) DependencyHealth
	version string
	now     clock.Func

	mu        sync.Mutex
	result    DependencyHealth
	expiresAt time.Time
}

func NewEndpoint(check func(context.Context) DependencyHealth, version string, now clock.Func) *Endpoint {
	return &Endpoint{check: check, version: version, now: now}
}

func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := background.Detach(r.Context(), endpointBudget)
	defer cancel()
	if e.cached(ctx).DB == DepDown {
		httpserver.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "version": e.version})
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": e.version})
}

func (e *Endpoint) cached(ctx context.Context) DependencyHealth {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.now().Before(e.expiresAt) {
		return e.result
	}
	e.result = e.check(ctx)
	e.expiresAt = e.now().Add(endpointCacheTTL)
	return e.result
}
