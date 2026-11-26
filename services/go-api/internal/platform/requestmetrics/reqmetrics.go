package requestmetrics

import "altune/go-api/internal/platform/requestmetrics/internal/core/registry"

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
