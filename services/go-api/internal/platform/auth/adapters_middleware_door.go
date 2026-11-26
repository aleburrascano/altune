package auth

import middleware "altune/go-api/internal/platform/auth/internal/adapters/middleware"

type FailureLimits = middleware.FailureLimits

var (
	Middleware        = middleware.Middleware
	WithFailureLimits = middleware.WithFailureLimits
	WithMetrics       = middleware.WithMetrics
)
