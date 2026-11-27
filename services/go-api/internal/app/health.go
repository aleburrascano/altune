package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/clock"
	"context"
)

var buildCommit = "unknown"

func BuildCommit() string { return buildCommit }

func (a *application) dependencyHealth(ctx context.Context) monitoring.DependencyHealth {
	return health.Check(ctx, clock.Now, a.probes, health.DefaultProbeTimeout)
}
