package app

import (
	"altune/go-api/internal/platform/auth"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"net/http"
)

func newAuthVerifier(ctx context.Context, cfg *config.Config, metrics *telemetry.Registry) (*auth.SupabaseJWTVerifier, error) {
	return auth.NewSupabaseJWTVerifier(
		ctx,
		cfg.SupabaseJWTJWKSURL,
		cfg.SupabaseProjectURL,
		cfg.SupabaseJWTAud,
		clock.Now,
		random.Real,
		auth.NewAuthMetrics(metrics),
		auth.NoopTokenRevoker(),
		auth.DefaultJWKSSchedule(),
	)
}

func authMiddleware(verifier auth.TokenVerifier, metrics *telemetry.Registry) func(http.Handler) http.Handler {
	return auth.MustMiddleware(verifier, clock.Now, auth.NewAuthMetrics(metrics), auth.DefaultFailureLimits)
}
