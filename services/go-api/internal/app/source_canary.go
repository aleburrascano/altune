package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/sourcecheck"
	"altune/go-api/internal/shared/clock"
	"context"
	"time"
)

const sourceCanaryInterval = time.Hour

func (a *App) startSourceCanary(ctx context.Context, probes sourcecheck.Probes) {
	checks := sourcecheck.Checks(probes)
	if len(checks) == 0 {
		return
	}
	a.startSimpleJob(ctx, jobs.AcquisitionSourceCanary, sourceCanaryInterval, func(ctx context.Context) error {
		return sourcecheck.Run(ctx, clock.Now, checks)
	}, "interval", sourceCanaryInterval.String())
}
