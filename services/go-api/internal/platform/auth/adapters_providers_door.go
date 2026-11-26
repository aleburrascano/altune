package auth

import providers "altune/go-api/internal/platform/auth/internal/adapters/providers"

type (
	JWKSSchedule        = providers.JWKSSchedule
	SupabaseJWTVerifier = providers.SupabaseJWTVerifier
)

var (
	DefaultJWKSSchedule    = providers.DefaultJWKSSchedule
	ErrJWKSBodyTooLarge    = providers.ErrJWKSBodyTooLarge
	ErrJWKSEmptyKeySet     = providers.ErrJWKSEmptyKeySet
	ErrJWKSRefreshBackoff  = providers.ErrJWKSRefreshBackoff
	ErrJWKSClockRequired   = providers.ErrJWKSClockRequired
	ErrJWKSStale           = providers.ErrJWKSStale
	NewSupabaseJWTVerifier = providers.NewSupabaseJWTVerifier
)
