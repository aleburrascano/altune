package app

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/auth/adapters/testauth"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/http"

	authMetrics "altune/go-api/internal/auth/adapters/metrics"
	authProviders "altune/go-api/internal/auth/adapters/providers"
	authPorts "altune/go-api/internal/auth/ports"

	"github.com/go-chi/chi/v5"
)

func newAuthVerifier(ctx context.Context, cfg *config.Config) (*authProviders.SupabaseJWTVerifier, error) {
	return authProviders.NewSupabaseJWTVerifier(
		ctx,
		cfg.SupabaseJWTJWKSURL,
		cfg.SupabaseProjectURL,
		cfg.SupabaseJWTAud,
		authProviders.WithJWKSMetrics(authMetrics.NewExpvarAuthMetrics()),
		authProviders.WithTokenRevoker(authPorts.NoopTokenRevoker()),
	)
}

func authMiddleware(verifier authPorts.TokenVerifier) func(http.Handler) http.Handler {
	return middleware.Middleware(verifier, middleware.WithMetrics(authMetrics.NewExpvarAuthMetrics()))
}

func buildTestAuthVerifier(cfg testAuthConfig, supa authPorts.TokenVerifier) (*testauth.TestAuth, authPorts.TokenVerifier, error) {
	if !cfg.TestAuthEnabled() {
		return nil, supa, nil
	}
	ta, err := testauth.New()
	if err != nil {
		return nil, nil, err
	}
	return ta, testauth.Combine(ta, supa), nil
}

type testAuthConfig interface {
	TestAuthEnabled() bool
}

func mountTestLogin(ctx context.Context, r chi.Router, ta *testauth.TestAuth) {
	slog.WarnContext(ctx, "test auth enabled: unauthenticated /test/login is mounted")
	r.Post("/test/login", ta.LoginHandler())
}
