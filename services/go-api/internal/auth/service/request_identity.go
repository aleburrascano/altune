package service

import "altune/go-api/internal/shared/requestuser"

var (
	ContextWithUserID      = requestuser.ContextWithUserID
	UserIDFromContext      = requestuser.UserIDFromContext
	ContextWithTokenExpiry = requestuser.ContextWithTokenExpiry
	TokenExpiryFromContext = requestuser.TokenExpiryFromContext
	UntilTokenExpiry       = requestuser.UntilTokenExpiry
)
