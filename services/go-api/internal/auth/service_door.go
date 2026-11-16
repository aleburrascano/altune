package auth

import service "altune/go-api/internal/auth/service"

var (
	ContextWithUserID = service.ContextWithUserID
	UserIDFromContext = service.UserIDFromContext
)
