package app

import (
	"context"
	"errors"
	"testing"
)

type stubAuthChecker struct {
	err error
}

func (s stubAuthChecker) CheckHealth(context.Context) error { return s.err }

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
