package app

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"time"
)

func newAuthVerifier(ctx context.Context, cfg *config.Config) (*auth.SupabaseJWTVerifier, error) {
	return auth.NewSupabaseJWTVerifier(
		ctx,
		cfg.SupabaseJWTJWKSURL,
		cfg.SupabaseProjectURL,
		cfg.SupabaseJWTAud,
		time.Now,
		auth.WithJWKSMetrics(auth.NewExpvarAuthMetrics()),
		auth.WithTokenRevoker(auth.NoopTokenRevoker()),
	)
}

func authMiddleware(verifier auth.TokenVerifier) func(http.Handler) http.Handler {
	return auth.MustMiddleware(verifier, time.Now, auth.WithMetrics(auth.NewExpvarAuthMetrics()))
}
