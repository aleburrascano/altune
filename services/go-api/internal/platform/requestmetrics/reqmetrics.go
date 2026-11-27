package requestmetrics

import "altune/go-api/internal/platform/requestmetrics/internal/core/registry"

type (
	Registry     = registry.Registry
	RouteLatency = registry.RouteLatency
	Snapshot     = registry.Snapshot
)

func NewRegistry() *Registry {
	return registry.New()
}
