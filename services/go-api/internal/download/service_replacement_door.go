package download

import (
	"altune/go-api/internal/download/internal/service"
)

var (
	UpgradeInterval       = service.PlanInterval
	NewReplacementPlanner = service.NewReplacementPlanner
)
