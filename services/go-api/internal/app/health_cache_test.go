package app

import (
	"altune/go-api/internal/shared/database"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestHandleHealth_ConcurrentRequestsShareOneProbe(t *testing.T) {
	var probes atomic.Int64
	a := &App{dbHealth: func(context.Context) database.HealthStatus {
		probes.Add(1)
		return database.HealthStatus{OK: true}
	}}

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			a.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
			}
		}()
	}
	wg.Wait()

	if got := probes.Load(); got != 1 {
		t.Errorf("db probes within TTL: got %d, want 1", got)
	}
}
