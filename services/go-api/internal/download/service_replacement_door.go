package download

import (
	"altune/go-api/internal/download/internal/service"
)

type UpgradeConfig = service.RunnerConfig

var (
	UpgradeInterval       = service.PlanInterval
	NewReplacementPlanner = service.NewReplacementPlanner
	InPlaceCheckOf        = service.InPlaceCheckOf
)
