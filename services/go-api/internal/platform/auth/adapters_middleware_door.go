package auth

import middleware "altune/go-api/internal/platform/auth/internal/adapters/middleware"

type FailureLimits = middleware.FailureLimits

var (
	DefaultFailureLimits       = middleware.DefaultFailureLimits
	ErrMiddlewareClockRequired = middleware.ErrClockRequired
	Middleware                 = middleware.Middleware
	MustMiddleware             = middleware.MustMiddleware
)
