package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func newAuthVerifier(ctx context.Context, cfg *config.Config) (*auth.SupabaseJWTVerifier, error) {
	return auth.NewSupabaseJWTVerifier(
		ctx,
		cfg.SupabaseJWTJWKSURL,
		cfg.SupabaseProjectURL,
		cfg.SupabaseJWTAud,
		auth.WithJWKSMetrics(auth.NewExpvarAuthMetrics()),
		auth.WithTokenRevoker(auth.NoopTokenRevoker()),
	)
}

func authMiddleware(verifier auth.TokenVerifier) func(http.Handler) http.Handler {
	return middleware.Middleware(verifier, middleware.WithMetrics(auth.NewExpvarAuthMetrics()))
}

func buildTestAuthVerifier(cfg testAuthConfig, supa auth.TokenVerifier) (*auth.TestAuth, auth.TokenVerifier, error) {
	if !cfg.TestAuthEnabled() {
		return nil, supa, nil
	}
	ta, err := auth.NewTestAuth()
	if err != nil {
		return nil, nil, err
	}
	return ta, auth.CombineTestVerifier(ta, supa), nil
}

type testAuthConfig interface {
	TestAuthEnabled() bool
}

func mountTestLogin(ctx context.Context, r chi.Router, ta *auth.TestAuth) {
	slog.WarnContext(ctx, "test auth enabled: unauthenticated /test/login is mounted")
	r.Post("/test/login", ta.LoginHandler())
}
