package app

import (
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"testing"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
)

type stubAuthChecker struct {
	err error
}

func (s stubAuthChecker) CheckHealth(context.Context) error { return s.err }

func TestDependencyHealth_ReportsRealDBError(t *testing.T) {
	// Regression for #398: a failing DB check must surface the live error
	// (pool exhaustion, auth, TLS, ...) the same way the Redis branch does,
	// not a fixed placeholder that leaves the operator blind.
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
	// Regression for #375: the plain /health route feeds dependencyHealth the
	// bare request context, which has no deadline. A stalled DB call (outage,
	// wedged pool) would otherwise hang the probe — and the endpoint — forever.
	// dependencyHealth must bound each dependency call itself so a hang is
	// reported as "down" within the probe timeout rather than blocking.
	a := &App{
		depProbeTimeout: 50 * time.Millisecond,
		dbHealth: func(ctx context.Context) database.HealthStatus {
			<-ctx.Done() // never returns unless the probe bounds the context
			return database.HealthStatus{OK: false, Err: ctx.Err()}
		},
	}

	done := make(chan adminHandler.DependencyHealth, 1)
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
	// pool and redisClient are nil, so DB and Redis report "not_configured"
	// (ready); the auth probe is the only moving part under test.
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
