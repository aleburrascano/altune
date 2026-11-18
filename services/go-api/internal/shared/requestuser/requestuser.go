package requestuser

import (
	"altune/go-api/internal/shared/requestuser/internal/adapters/httpuser"
	"altune/go-api/internal/shared/requestuser/internal/core/identity"
)

var ErrTokenExpired = identity.ErrTokenExpired

var (
	ContextWithUserID      = identity.ContextWithUserID
	UserIDFromContext      = identity.UserIDFromContext
	ContextWithTokenExpiry = identity.ContextWithTokenExpiry
	TokenExpiryFromContext = identity.TokenExpiryFromContext
	UntilTokenExpiry       = identity.UntilTokenExpiry
	RequireUserID          = httpuser.RequireUserID
	ClientKey              = httpuser.ClientKey
)
