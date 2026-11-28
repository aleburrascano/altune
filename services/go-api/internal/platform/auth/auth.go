package auth

import (
	"altune/go-api/internal/platform/auth/internal/adapters/handler"
	"altune/go-api/internal/platform/auth/internal/adapters/supabase"
	authtelemetry "altune/go-api/internal/platform/auth/internal/adapters/telemetry"
	"altune/go-api/internal/platform/auth/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/random"
	"context"
	"fmt"
)

type (
	SupabaseJWTVerifier = supabase.SupabaseJWTVerifier
	TokenVerifier       = ports.TokenVerifier

	authMetrics  = ports.AuthMetrics
	jwksSchedule = supabase.JWKSSchedule
	tokenRevoker = ports.TokenRevoker
)

var (
	DefaultFailureLimits = handler.DefaultFailureLimits
	DefaultJWKSSchedule  = supabase.DefaultJWKSSchedule
	MustMiddleware       = handler.MustMiddleware
	NewAuthMetrics       = authtelemetry.NewAuthMetrics
	NoopTokenRevoker     = ports.NoopTokenRevoker
	ReadSnapshot         = authtelemetry.ReadSnapshot
)

func New(
	ctx context.Context,
	jwksURL, projectURL, audience string,
	now clock.Func,
	jitter random.Unit,
	metrics authMetrics,
	revoker tokenRevoker,
	schedule jwksSchedule,
) (*SupabaseJWTVerifier, error) {
	verifier, err := supabase.NewSupabaseJWTVerifier(ctx, jwksURL, projectURL, audience, now, jitter, metrics, revoker, schedule)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	return verifier, nil
}
