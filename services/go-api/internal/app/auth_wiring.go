package app

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/random"
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
		random.Real,
		auth.NewExpvarAuthMetrics(),
		auth.NoopTokenRevoker(),
		auth.DefaultJWKSSchedule(),
	)
}

func authMiddleware(verifier auth.TokenVerifier) func(http.Handler) http.Handler {
	return auth.MustMiddleware(verifier, time.Now, auth.NewExpvarAuthMetrics(), auth.DefaultFailureLimits)
}
