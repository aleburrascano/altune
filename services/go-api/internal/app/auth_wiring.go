package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"

	authMetrics "altune/go-api/internal/auth/adapters/metrics"
	authProviders "altune/go-api/internal/auth/adapters/providers"
)

func newAuthVerifier(ctx context.Context, cfg *config.Config) (*authProviders.SupabaseJWTVerifier, error) {
	return authProviders.NewSupabaseJWTVerifier(
		ctx,
		cfg.SupabaseJWTJWKSURL,
		cfg.SupabaseProjectURL,
		cfg.SupabaseJWTAud,
		authProviders.WithJWKSMetrics(authMetrics.NewExpvarAuthMetrics()),
	)
}

// authMiddleware is auth.Middleware with token-rejection and
// verifier-unavailable counters wired to the expvar adapter. Every route group
// that authenticates uses it, so no group's 401s or 503s go uncounted.
func authMiddleware(verifier auth.TokenVerifier) func(http.Handler) http.Handler {
	return auth.Middleware(verifier, auth.WithMetrics(authMetrics.NewExpvarAuthMetrics()))
}
