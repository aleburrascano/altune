package observe

import handler "altune/go-api/internal/observe/internal/adapters/handler"

type (
	AcquisitionReader = handler.AcquisitionReader
	DepStatus         = handler.DepStatus
	DependencyDetail  = handler.DependencyDetail
	DependencyHealth  = handler.DependencyHealth
	Deps              = handler.Deps
	Handler           = handler.Handler
	LiveMetrics       = handler.LiveMetrics
	LiveMetricsSource = handler.LiveMetricsSource
)

var (
	Gate              = handler.Gate
	NewObserveHandler = handler.NewObserveHandler
	NoStoreAndNosniff = handler.NoStoreAndNosniff
)

const (
	DepDown          = handler.DepDown
	DepNotConfigured = handler.DepNotConfigured
	DepUp            = handler.DepUp
)
