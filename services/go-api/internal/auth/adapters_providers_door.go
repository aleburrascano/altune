package auth

import providers "altune/go-api/internal/auth/internal/adapters/providers"

type SupabaseJWTVerifier = providers.SupabaseJWTVerifier

var (
	NewSupabaseJWTVerifier = providers.NewSupabaseJWTVerifier
	WithJWKSMetrics        = providers.WithJWKSMetrics
	WithTokenRevoker       = providers.WithTokenRevoker
)
