package reqmetrics

import (
	"altune/go-api/internal/shared/reqmetrics/internal/core/registry"
	"time"
)

type (
	Snapshot      = registry.Snapshot
	RouteLatency  = registry.RouteLatency
	BucketCount   = registry.BucketCount
	StatusClasses = registry.StatusClasses
)

var defaultRegistry = registry.New()

func Observe(route string, d time.Duration, status int) {
	defaultRegistry.Observe(route, d, status)
}

func ReadSnapshot() Snapshot {
	return defaultRegistry.Snapshot()
}
