package handler

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// stalledRollupStore models a stalled DB query: MetricsHistory only returns once
// its context is done, and then reports the context's error.
type stalledRollupStore struct{}

func (stalledRollupStore) RollupDay(context.Context, time.Time) error { return nil }

func (stalledRollupStore) MetricsHistory(ctx context.Context, _ string, _ int) ([]ports.MetricPoint, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestAdminMetricsHistory_StalledStoreReturnsGatewayTimeout checks that a
// stalled metrics-history query cannot park the /admin/metrics request: the
// handler bounds the call and maps the deadline to a coded 504.
func TestAdminMetricsHistory_StalledStoreReturnsGatewayTimeout(t *testing.T) {
	h := New(nil, nil).WithMetricsHistory(stalledRollupStore{})
	h.metricsHistoryTimeout = 20 * time.Millisecond

	r := chi.NewRouter()
	h.RegisterData(r)

	req := httptest.NewRequest(http.MethodGet, "/metrics?metric=search.p95&days=7", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		r.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("metrics history request hung on a stalled store")
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504; body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Code != "admin.metrics_history_timeout" {
		t.Errorf("code = %q, want admin.metrics_history_timeout", body.Code)
	}
}

func TestAdminMetricsHistory_DefaultTimeoutIsBounded(t *testing.T) {
	h := New(nil, nil)
	if h.metricsHistoryTimeout <= 0 || h.metricsHistoryTimeout > 30*time.Second {
		t.Errorf("metricsHistoryTimeout = %v, want a positive bound <= 30s", h.metricsHistoryTimeout)
	}
}
