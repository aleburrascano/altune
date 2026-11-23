package reqmetrics

import "altune/go-api/internal/shared/reqmetrics/internal/core/registry"

type (
	Registry      = registry.Registry
	Snapshot      = registry.Snapshot
	RouteLatency  = registry.RouteLatency
	BucketCount   = registry.BucketCount
	StatusClasses = registry.StatusClasses
)

func NewRegistry() *Registry {
	return registry.New()
}
