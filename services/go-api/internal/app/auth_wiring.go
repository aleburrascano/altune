package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
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
	return auth.Middleware(verifier, auth.WithMetrics(auth.NewExpvarAuthMetrics()))
}
