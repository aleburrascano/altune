package app

import (
	observeHandler "altune/go-api/internal/observe/handler"
	"altune/go-api/internal/shared/database"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubAuthChecker struct {
	err error
}

func (s stubAuthChecker) CheckHealth(context.Context) error { return s.err }

func TestDependencyHealth_ReportsRealDBError(t *testing.T) {
	wantErr := "connection refused: pool exhausted"
	a := &App{dbHealth: func(context.Context) database.HealthStatus {
		return database.HealthStatus{OK: false, Err: errors.New(wantErr)}
	}}

	health := a.dependencyHealth(context.Background())

	if health.DB != "down" {
		t.Errorf("db status: got %q, want %q", health.DB, "down")
	}
	if health.Detail.DBError != wantErr {
		t.Errorf("db error: got %q, want real error %q", health.Detail.DBError, wantErr)
	}
	if health.Healthy() {
		t.Error("expected dependency health to be unhealthy when db is down")
	}
}

func TestDependencyHealth_HangingDBRespectsTimeout(t *testing.T) {
	a := &App{
		depProbeTimeout: 50 * time.Millisecond,
		dbHealth: func(ctx context.Context) database.HealthStatus {
			<-ctx.Done()
			return database.HealthStatus{OK: false, Err: ctx.Err()}
		},
	}

	done := make(chan observeHandler.DependencyHealth, 1)
	go func() { done <- a.dependencyHealth(context.Background()) }()

	select {
	case health := <-done:
		if health.DB != "down" {
			t.Errorf("db status: got %q, want %q", health.DB, "down")
		}
		if health.Healthy() {
			t.Error("expected dependency health to be unhealthy when db probe times out")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dependencyHealth did not return within bound: DB probe is unbounded")
	}
}

func TestDependencyHealth_HealthyDB(t *testing.T) {
	a := &App{dbHealth: func(context.Context) database.HealthStatus {
		return database.HealthStatus{OK: true}
	}}

	health := a.dependencyHealth(context.Background())

	if health.DB != "ok" {
		t.Errorf("db status: got %q, want %q", health.DB, "ok")
	}
	if health.Detail.DBError != "" {
		t.Errorf("db error: got %q, want empty", health.Detail.DBError)
	}
}

func TestDependencyHealth_DBNotConfigured(t *testing.T) {
	a := &App{}

	health := a.dependencyHealth(context.Background())

	if health.DB != "not_configured" {
		t.Errorf("db status: got %q, want %q", health.DB, "not_configured")
	}
}

func TestDependencyHealth_ReflectsAuthDegradation(t *testing.T) {
	a := &App{authVerifier: stubAuthChecker{err: errors.New("fetch JWKS: boom")}}

	health := a.dependencyHealth(context.Background())

	if health.Auth != "down" {
		t.Errorf("auth status: got %q, want %q", health.Auth, "down")
	}
	if health.Healthy() {
		t.Error("expected dependency health to be unhealthy when auth is down")
	}
	if health.Detail.AuthError == "" {
		t.Error("expected AuthError detail to be populated when auth is down")
	}
}

func TestDependencyHealth_HealthyAuth(t *testing.T) {
	a := &App{authVerifier: stubAuthChecker{err: nil}}

	health := a.dependencyHealth(context.Background())

	if health.Auth != "ok" {
		t.Errorf("auth status: got %q, want %q", health.Auth, "ok")
	}
	if !health.Healthy() {
		t.Error("expected dependency health to be healthy when auth is ok")
	}
}

func TestDependencyHealth_AuthNotConfigured(t *testing.T) {
	a := &App{}

	health := a.dependencyHealth(context.Background())

	if health.Auth != "not_configured" {
		t.Errorf("auth status: got %q, want %q", health.Auth, "not_configured")
	}
	if !health.Healthy() {
		t.Error("expected dependency health to be healthy when auth is not configured")
	}
}

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

func TestHealthCache_ReprobesOnceClockPassesTTL(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cache := &healthCache{now: func() time.Time { return current }}
	probes := 0
	probe := func() observeHandler.DependencyHealth {
		probes++
		return observeHandler.DependencyHealth{}
	}

	cache.get(probe)
	current = current.Add(healthCacheTTL - time.Nanosecond)
	cache.get(probe)
	if probes != 1 {
		t.Fatalf("probes just inside TTL: got %d, want 1", probes)
	}

	current = current.Add(time.Nanosecond)
	cache.get(probe)
	if probes != 2 {
		t.Errorf("probes once TTL elapsed: got %d, want 2", probes)
	}
}

func TestHandleHealth_ReportsVersion(t *testing.T) {
	original := buildCommit
	buildCommit = "abc123deadbeef"
	defer func() { buildCommit = original }()

	var okBody struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	okApp := &App{}
	okRec := httptest.NewRecorder()
	okApp.handleHealth(okRec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if err := json.NewDecoder(okRec.Body).Decode(&okBody); err != nil {
		t.Fatalf("decode ok response: %v", err)
	}
	if okRec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", okRec.Code, http.StatusOK)
	}
	if okBody.Version != buildCommit {
		t.Errorf("version: got %q, want %q", okBody.Version, buildCommit)
	}

	var degradedBody struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	degradedApp := &App{dbHealth: func(context.Context) database.HealthStatus {
		return database.HealthStatus{OK: false, Err: errors.New("boom")}
	}}
	degradedRec := httptest.NewRecorder()
	degradedApp.handleHealth(degradedRec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if err := json.NewDecoder(degradedRec.Body).Decode(&degradedBody); err != nil {
		t.Fatalf("decode degraded response: %v", err)
	}
	if degradedRec.Code != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want %d", degradedRec.Code, http.StatusServiceUnavailable)
	}
	if degradedBody.Version != buildCommit {
		t.Errorf("version: got %q, want %q", degradedBody.Version, buildCommit)
	}
}
