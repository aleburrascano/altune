package auth

import providers "altune/go-api/internal/auth/internal/adapters/providers"

type (
	SupabaseJWTVerifier       = providers.SupabaseJWTVerifier
	SupabaseJWTVerifierOption = providers.SupabaseJWTVerifierOption
)

var (
	ErrJWKSBodyTooLarge       = providers.ErrJWKSBodyTooLarge
	ErrJWKSEmptyKeySet        = providers.ErrJWKSEmptyKeySet
	ErrJWKSRefreshBackoff     = providers.ErrJWKSRefreshBackoff
	ErrJWKSStale              = providers.ErrJWKSStale
	NewSupabaseJWTVerifier    = providers.NewSupabaseJWTVerifier
	WithJWKSBackgroundRefresh = providers.WithJWKSBackgroundRefresh
	WithJWKSClock             = providers.WithJWKSClock
	WithJWKSFetchTimeout      = providers.WithJWKSFetchTimeout
	WithJWKSMetrics           = providers.WithJWKSMetrics
	WithTokenRevoker          = providers.WithTokenRevoker
)
