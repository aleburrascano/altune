package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/download"
	"context"
)

func (a *application) startAcquisitionUpgrade(ctx context.Context, module *download.Module) {
	plan, interval := module.Upgrade()
	if plan == nil {
		return
	}
	a.jobs.StartTicker(ctx, jobs.AcquisitionUpgrade, interval, plan,
		"interval", interval.String())
}
