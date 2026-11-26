package health_test

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func up(context.Context) error { return nil }

func failing(msg string) func(context.Context) error {
	return func(context.Context) error { return errors.New(msg) }
}

func TestCheck_ReportsTheRealDBError(t *testing.T) {
	wantErr := "connection refused: pool exhausted"

	got := health.Check(context.Background(), health.Probes{DB: failing(wantErr)}, time.Second)

	if got.DB != monitoring.DepDown || got.Detail.DBError != wantErr {
		t.Errorf("db = %q error %q, want down with %q", got.DB, got.Detail.DBError, wantErr)
	}
	if got.Healthy() {
		t.Error("dependency health is healthy with the db down")
	}
}

func TestCheck_AHangingProbeIsCutOffAtTheTimeout(t *testing.T) {
	hang := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan monitoring.DependencyHealth, 1)

	go func() {
		done <- health.Check(context.Background(), health.Probes{DB: hang, Redis: hang, Auth: hang}, 50*time.Millisecond)
	}()

	select {
	case got := <-done:
		if got.DB != monitoring.DepDown || got.Redis != monitoring.DepDown || got.Auth != monitoring.DepDown {
			t.Errorf("statuses = %q/%q/%q, want every hung probe down", got.DB, got.Redis, got.Auth)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Check did not return within 2s: a probe is unbounded")
	}
}

func TestCheck_TheDefaultTimeoutGivesEachProbeTwoSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		takes := func(d time.Duration) func(context.Context) error {
			return func(ctx context.Context) error {
				select {
				case <-time.After(d):
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		got := health.Check(context.Background(), health.Probes{
			DB:    takes(2*time.Second - time.Millisecond),
			Redis: takes(2*time.Second + time.Millisecond),
		}, health.DefaultProbeTimeout)

		if got.DB != monitoring.DepUp || got.Redis != monitoring.DepDown {
			t.Errorf("DB %q after 1.999s, Redis %q after 2.001s; want up then down at the 2s default", got.DB, got.Redis)
		}
	})
}

func TestCheck_HealthyProbesAreUpWithNoError(t *testing.T) {
	got := health.Check(context.Background(), health.Probes{DB: up, Redis: up, Auth: up}, time.Second)

	if got.DB != monitoring.DepUp || got.Redis != monitoring.DepUp || got.Auth != monitoring.DepUp {
		t.Errorf("statuses = %q/%q/%q, want all ok", got.DB, got.Redis, got.Auth)
	}
	if got.Detail.DBError != "" || got.Detail.RedisError != "" || got.Detail.AuthError != "" || !got.Healthy() {
		t.Errorf("detail = %+v, want no errors and healthy", got.Detail)
	}
}

func TestCheck_AnAbsentProbeIsNotConfiguredAndHealthy(t *testing.T) {
	got := health.Check(context.Background(), health.Probes{}, time.Second)

	if got.DB != monitoring.DepNotConfigured || got.Redis != monitoring.DepNotConfigured || got.Auth != monitoring.DepNotConfigured {
		t.Errorf("statuses = %q/%q/%q, want all not_configured", got.DB, got.Redis, got.Auth)
	}
	if !got.Healthy() {
		t.Error("unconfigured dependencies are not healthy")
	}
}

func TestCheck_EachDependencyReportsItsOwnFailure(t *testing.T) {
	got := health.Check(context.Background(), health.Probes{DB: up, Redis: failing("redis refused"), Auth: failing("fetch JWKS: boom")}, time.Second)

	if got.DB != monitoring.DepUp || got.Redis != monitoring.DepDown || got.Auth != monitoring.DepDown {
		t.Errorf("statuses = %q/%q/%q, want ok/down/down", got.DB, got.Redis, got.Auth)
	}
	if got.Detail.DBError != "" || got.Detail.RedisError != "redis refused" || got.Detail.AuthError != "fetch JWKS: boom" {
		t.Errorf("detail = %+v, want each error under its own dependency", got.Detail)
	}
}

func TestCheck_LatencyIsTheProbesOwnDuration(t *testing.T) {
	slow := func(context.Context) error {
		time.Sleep(30 * time.Millisecond)
		return nil
	}

	got := health.Check(context.Background(), health.Probes{DB: slow, Redis: slow, Auth: slow}, time.Second)

	for name, ms := range map[string]int64{"db": got.Detail.DBLatencyMs, "redis": got.Detail.RedisLatencyMs, "auth": got.Detail.AuthLatencyMs} {
		if ms < 30 || ms > 1000 {
			t.Errorf("%s latency = %dms, want the probe's ~30ms", name, ms)
		}
	}
}

func TestCheck_StampsTheCheckInUTC(t *testing.T) {
	before := time.Now()

	got := health.Check(context.Background(), health.Probes{}, time.Second)

	if got.Detail.CheckedAt.Location() != time.UTC || got.Detail.CheckedAt.Before(before.Add(-time.Second)) {
		t.Errorf("checked at = %v, want now in UTC", got.Detail.CheckedAt)
	}
}

type healthBody struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func serve(t *testing.T, endpoint http.Handler) (int, healthBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	endpoint.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	var body healthBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rec.Code, body
}

func checkWith(status monitoring.DependencyHealth) func(context.Context) monitoring.DependencyHealth {
	return func(context.Context) monitoring.DependencyHealth { return status }
}

func TestEndpoint_ReportsTheVersionHealthyOrDegraded(t *testing.T) {
	healthy := health.NewEndpoint(checkWith(monitoring.DependencyHealth{DB: monitoring.DepUp}), "abc123deadbeef", time.Now)
	degraded := health.NewEndpoint(checkWith(monitoring.DependencyHealth{DB: monitoring.DepDown}), "abc123deadbeef", time.Now)

	if code, body := serve(t, healthy); code != http.StatusOK || body != (healthBody{Status: "ok", Version: "abc123deadbeef"}) {
		t.Errorf("healthy = %d %+v, want 200 ok with the version", code, body)
	}
	if code, body := serve(t, degraded); code != http.StatusServiceUnavailable || body != (healthBody{Status: "degraded", Version: "abc123deadbeef"}) {
		t.Errorf("degraded = %d %+v, want 503 degraded with the version", code, body)
	}
}

func TestEndpoint_OnlyADownDatabaseFailsTheCheck(t *testing.T) {
	cases := map[string]struct {
		status monitoring.DependencyHealth
		want   int
	}{
		"only redis down":           {monitoring.DependencyHealth{DB: monitoring.DepUp, Redis: monitoring.DepDown, Auth: monitoring.DepUp}, http.StatusOK},
		"only auth stale":           {monitoring.DependencyHealth{DB: monitoring.DepUp, Auth: monitoring.DepDown}, http.StatusOK},
		"db not configured":         {monitoring.DependencyHealth{DB: monitoring.DepNotConfigured}, http.StatusOK},
		"db down despite others up": {monitoring.DependencyHealth{DB: monitoring.DepDown, Redis: monitoring.DepUp, Auth: monitoring.DepUp}, http.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if code, _ := serve(t, health.NewEndpoint(checkWith(tc.status), "v", time.Now)); code != tc.want {
				t.Errorf("status = %d, want %d", code, tc.want)
			}
		})
	}
}

func TestEndpoint_ConcurrentRequestsShareOneCheck(t *testing.T) {
	var checks atomic.Int64
	endpoint := health.NewEndpoint(func(context.Context) monitoring.DependencyHealth {
		checks.Add(1)
		return monitoring.DependencyHealth{DB: monitoring.DepUp}
	}, "v", time.Now)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			endpoint.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
		})
	}
	wg.Wait()

	if got := checks.Load(); got != 1 {
		t.Errorf("checks within the cache window = %d, want 1", got)
	}
}

func TestEndpoint_ChecksAgainOnceTwoSecondsHavePassed(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	checks := 0
	endpoint := health.NewEndpoint(func(context.Context) monitoring.DependencyHealth {
		checks++
		return monitoring.DependencyHealth{DB: monitoring.DepUp}
	}, "v", func() time.Time { return current })

	serve(t, endpoint)
	current = current.Add(2*time.Second - time.Nanosecond)
	serve(t, endpoint)
	if checks != 1 {
		t.Fatalf("checks just inside the cache window = %d, want 1", checks)
	}

	current = current.Add(time.Nanosecond)
	serve(t, endpoint)
	if checks != 2 {
		t.Errorf("checks once the window elapsed = %d, want 2", checks)
	}
}

func TestEndpoint_TheCheckOutlivesACanceledRequest(t *testing.T) {
	var checkErr error
	endpoint := health.NewEndpoint(func(ctx context.Context) monitoring.DependencyHealth {
		checkErr = ctx.Err()
		return monitoring.DependencyHealth{DB: monitoring.DepUp}
	}, "v", time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	endpoint.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx))

	if checkErr != nil {
		t.Errorf("check ran with a canceled context (%v), want one that outlives the request", checkErr)
	}
}

func TestEndpoint_TheDetachedCheckIsBoundedAtThreeProbeTimeouts(t *testing.T) {
	var remaining time.Duration
	var hasDeadline bool
	endpoint := health.NewEndpoint(func(ctx context.Context) monitoring.DependencyHealth {
		var deadline time.Time
		deadline, hasDeadline = ctx.Deadline()
		remaining = time.Until(deadline)
		return monitoring.DependencyHealth{DB: monitoring.DepUp}
	}, "v", time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	endpoint.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx))

	budget := 3 * health.DefaultProbeTimeout
	if !hasDeadline {
		t.Fatal("the detached check has no deadline, want it bounded")
	}
	if remaining > budget || remaining < budget-time.Second {
		t.Errorf("the detached check had %s left, want about %s so three sequential probes each get their full timeout", remaining, budget)
	}
}

func TestEndpoint_TheDetachedCheckKeepsTheRequestsCorrelationID(t *testing.T) {
	var seen string
	endpoint := health.NewEndpoint(func(ctx context.Context) monitoring.DependencyHealth {
		seen = logging.CorrelationIDFromContext(ctx)
		return monitoring.DependencyHealth{DB: monitoring.DepUp}
	}, "v", time.Now)
	ctx, cancel := context.WithCancel(logging.ContextWithCorrelationID(context.Background(), "corr-health-1"))
	cancel()

	endpoint.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil).WithContext(ctx))

	if seen != "corr-health-1" {
		t.Errorf("correlation id seen by the check = %q, want %q so its probes log under the request that ran them", seen, "corr-health-1")
	}
}
