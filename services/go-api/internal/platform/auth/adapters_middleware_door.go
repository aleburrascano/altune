package auth

import middleware "altune/go-api/internal/platform/auth/internal/adapters/middleware"

type FailureLimits = middleware.FailureLimits

var (
	ErrMiddlewareClockRequired = middleware.ErrClockRequired
	Middleware                 = middleware.Middleware
	MustMiddleware             = middleware.MustMiddleware
	WithFailureLimits          = middleware.WithFailureLimits
	WithMetrics                = middleware.WithMetrics
)
