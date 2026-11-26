package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/app/sourcecheck"
	"context"
	"time"
)

const sourceCanaryInterval = time.Hour

func (a *App) startSourceCanary(probes sourcecheck.Probes) {
	checks := sourcecheck.Checks(probes)
	if len(checks) == 0 {
		return
	}
	a.startSimpleJob(jobs.AcquisitionSourceCanary, sourceCanaryInterval, func(ctx context.Context) error {
		return sourcecheck.Run(ctx, checks)
	}, "interval", sourceCanaryInterval.String())
}
