package requestmetrics

import (
	"altune/go-api/internal/platform/requestmetrics/internal/adapters/observer"
	"altune/go-api/internal/platform/requestmetrics/internal/service"
)

type (
	Registry = service.Registry
	Snapshot = service.Snapshot
)

var Observer = observer.New

func New() *Registry {
	return service.New()
}
