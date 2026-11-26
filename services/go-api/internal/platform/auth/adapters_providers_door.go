package auth

import providers "altune/go-api/internal/platform/auth/internal/adapters/providers"

type (
	SupabaseJWTVerifier       = providers.SupabaseJWTVerifier
	SupabaseJWTVerifierOption = providers.SupabaseJWTVerifierOption
)

var (
	ErrJWKSBodyTooLarge       = providers.ErrJWKSBodyTooLarge
	ErrJWKSEmptyKeySet        = providers.ErrJWKSEmptyKeySet
	ErrJWKSRefreshBackoff     = providers.ErrJWKSRefreshBackoff
	ErrJWKSClockRequired      = providers.ErrJWKSClockRequired
	ErrJWKSStale              = providers.ErrJWKSStale
	NewSupabaseJWTVerifier    = providers.NewSupabaseJWTVerifier
	WithJWKSBackgroundRefresh = providers.WithJWKSBackgroundRefresh
	WithJWKSFetchTimeout      = providers.WithJWKSFetchTimeout
	WithJWKSMetrics           = providers.WithJWKSMetrics
	WithTokenRevoker          = providers.WithTokenRevoker
)
