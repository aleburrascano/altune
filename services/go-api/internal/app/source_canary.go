package app

import (
	"altune/go-api/internal/app/canary"
	"altune/go-api/internal/app/jobs"
	"context"
	"time"
)

const sourceCanaryInterval = time.Hour

func (a *App) startSourceCanary(probes canary.Probes) {
	checks := canary.Checks(probes)
	if len(checks) == 0 {
		return
	}
	a.startSimpleJob(jobs.AcquisitionSourceCanary, sourceCanaryInterval, func(ctx context.Context) error {
		return canary.Run(ctx, checks)
	}, "interval", sourceCanaryInterval.String())
}
