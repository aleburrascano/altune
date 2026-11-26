package auth

import middleware "altune/go-api/internal/platform/auth/internal/adapters/middleware"

var (
	DefaultFailureLimits = middleware.DefaultFailureLimits
	MustMiddleware       = middleware.MustMiddleware
)
