package app

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"

	authMetrics "altune/go-api/internal/auth/adapters/metrics"
	authProviders "altune/go-api/internal/auth/adapters/providers"
	authPorts "altune/go-api/internal/auth/ports"
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
