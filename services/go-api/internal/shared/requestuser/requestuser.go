package requestuser

import (
	"altune/go-api/internal/shared/requestuser/internal/adapters/httpuser"
	"altune/go-api/internal/shared/requestuser/internal/core/signedinuser"
)

var (
	ErrTokenExpired = signedinuser.ErrTokenExpired
)

var (
	ContextWithUserID      = signedinuser.ContextWithUserID
	UserIDFromContext      = signedinuser.UserIDFromContext
	ContextWithTokenExpiry = signedinuser.ContextWithTokenExpiry
	TokenExpiryFromContext = signedinuser.TokenExpiryFromContext
	UntilTokenExpiry       = signedinuser.UntilTokenExpiry
	UserID                 = httpuser.UserID
	ClientKey              = httpuser.ClientKey
	LimitPerUser           = httpuser.LimitPerUser
)
