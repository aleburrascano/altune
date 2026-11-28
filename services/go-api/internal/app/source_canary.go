package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/download"
	"context"
	"time"
)

const sourceCanaryInterval = time.Hour

func (a *application) startSourceCanary(ctx context.Context, module *download.Module) {
	if !module.HasSourceChecks() {
		return
	}
	a.startSimpleJob(ctx, jobs.AcquisitionSourceCanary, sourceCanaryInterval, module.CheckSources,
		"interval", sourceCanaryInterval.String())
}
