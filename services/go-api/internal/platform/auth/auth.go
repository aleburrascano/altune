package auth

import (
	"altune/go-api/internal/platform/auth/internal/adapters/handler"
	"altune/go-api/internal/platform/auth/internal/adapters/supabase"
	authtelemetry "altune/go-api/internal/platform/auth/internal/adapters/telemetry"
	"altune/go-api/internal/platform/auth/internal/ports"
)

type (
	SupabaseJWTVerifier = supabase.SupabaseJWTVerifier
	TokenVerifier       = ports.TokenVerifier
)

var (
	DefaultFailureLimits   = handler.DefaultFailureLimits
	DefaultJWKSSchedule    = supabase.DefaultJWKSSchedule
	MustMiddleware         = handler.MustMiddleware
	NewAuthMetrics         = authtelemetry.NewAuthMetrics
	NewSupabaseJWTVerifier = supabase.NewSupabaseJWTVerifier
	NoopTokenRevoker       = ports.NoopTokenRevoker
	ReadSnapshot           = authtelemetry.ReadSnapshot
)
