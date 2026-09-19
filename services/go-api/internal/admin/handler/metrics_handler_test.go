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

// recordingRollupStore captures the days the handler hands the store, which is
// the value the repo spends as the query's LIMIT.
type recordingRollupStore struct{ gotDays int }

func (*recordingRollupStore) RollupDay(context.Context, time.Time) error { return nil }

func (s *recordingRollupStore) MetricsHistory(_ context.Context, _ string, days int) ([]ports.MetricPoint, error) {
	s.gotDays = days
	return []ports.MetricPoint{}, nil
}

// TestAdminMetricsHistory_HostileDaysReachStoreClamped proves days is clamped
// before it reaches the store: a huge or garbage value cannot widen the LIMIT
// past the cap, and an in-range value still passes through untouched.
func TestAdminMetricsHistory_HostileDaysReachStoreClamped(t *testing.T) {
	cases := []struct {
		raw      string
		wantDays int
	}{
		{raw: "", wantDays: defaultMetricsHistoryDays},
		{raw: "-5", wantDays: defaultMetricsHistoryDays},
		{raw: "0", wantDays: defaultMetricsHistoryDays},
		{raw: "not-a-number", wantDays: defaultMetricsHistoryDays},
		{raw: "100000", wantDays: maxMetricsHistoryDays},
		{raw: "999999999", wantDays: maxMetricsHistoryDays},
		{raw: "366", wantDays: maxMetricsHistoryDays},
		{raw: "365", wantDays: maxMetricsHistoryDays},
		{raw: "7", wantDays: 7},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			store := &recordingRollupStore{}
			r := chi.NewRouter()
			New(nil, nil).WithMetricsHistory(store).RegisterData(r)

			req := httptest.NewRequest(http.MethodGet, "/metrics?metric=search.p95&days="+tc.raw, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
			}
			if store.gotDays != tc.wantDays {
				t.Errorf("days reaching store = %d, want %d", store.gotDays, tc.wantDays)
			}
		})
	}
}

func TestAdminMetricsHistory_DefaultTimeoutIsBounded(t *testing.T) {
	h := New(nil, nil)
	if h.metricsHistoryTimeout <= 0 || h.metricsHistoryTimeout > 30*time.Second {
		t.Errorf("metricsHistoryTimeout = %v, want a positive bound <= 30s", h.metricsHistoryTimeout)
	}
}
