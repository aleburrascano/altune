package app

import (
	"altune/go-api/internal/app/health"
	"altune/go-api/internal/platform/monitoring"
	"context"
)

var buildCommit = "unknown"

func BuildCommit() string { return buildCommit }

func (a *App) dependencyHealth(ctx context.Context) monitoring.DependencyHealth {
	return health.Check(ctx, a.probes, health.DefaultProbeTimeout)
}
