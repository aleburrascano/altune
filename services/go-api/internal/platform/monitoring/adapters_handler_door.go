package monitoring

import handler "altune/go-api/internal/platform/monitoring/internal/adapters/handler"

type (
	DepStatus        = handler.DepStatus
	DependencyDetail = handler.DependencyDetail
	DependencyHealth = handler.DependencyHealth
	Deps             = handler.Deps
	Handler          = handler.Handler
	LiveMetrics      = handler.LiveMetrics
	StreamBounds     = handler.StreamBounds
)

var (
	DefaultStreamBounds = handler.DefaultStreamBounds
	Gate                = handler.Gate
	NewObserveHandler   = handler.NewObserveHandler
)

const (
	DepDown          = handler.DepDown
	DepNotConfigured = handler.DepNotConfigured
	DepUp            = handler.DepUp
)
