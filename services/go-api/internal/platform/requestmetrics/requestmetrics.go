package requestmetrics

import (
	"altune/go-api/internal/platform/requestmetrics/internal/adapters/observer"
	"altune/go-api/internal/platform/requestmetrics/internal/domain"
)

type (
	Registry = domain.Registry
	Snapshot = domain.Snapshot
)

var Observer = observer.New

func NewRegistry() *Registry {
	return domain.New()
}
