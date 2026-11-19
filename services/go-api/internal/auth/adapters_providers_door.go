package auth

import providers "altune/go-api/internal/auth/internal/adapters/providers"

type (
	SupabaseJWTVerifier       = providers.SupabaseJWTVerifier
	SupabaseJWTVerifierOption = providers.SupabaseJWTVerifierOption
)

var (
	NewSupabaseJWTVerifier    = providers.NewSupabaseJWTVerifier
	WithJWKSBackgroundRefresh = providers.WithJWKSBackgroundRefresh
	WithJWKSClock             = providers.WithJWKSClock
	WithJWKSFetchTimeout      = providers.WithJWKSFetchTimeout
	WithJWKSMetrics           = providers.WithJWKSMetrics
	WithTokenRevoker          = providers.WithTokenRevoker
)
