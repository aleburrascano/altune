package auth

import middleware "altune/go-api/internal/auth/adapters/middleware"

var (
	Middleware  = middleware.Middleware
	WithMetrics = middleware.WithMetrics
)
