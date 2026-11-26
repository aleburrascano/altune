package app

import (
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/download"
	"context"
)

func (a *App) startAcquisitionUpgrade(ctx context.Context, scheduler *download.BackgroundAcquisitionScheduler) {
	if a.pool == nil || scheduler == nil {
		return
	}
	planner := download.NewReplacementPlanner(download.NewPostgresUpgradeStore(a.pool), scheduler, a.cfg.AcquisitionUpgradeConcurrency, a.cfg.AcquisitionUpgradeRetryAfter).CheckingUploads().
		RescuingLowBitrate(a.cfg.AcquisitionLowBitrateFloorKbps)
	interval := download.UpgradeInterval(a.cfg.AcquisitionUpgradePerHour)
	a.jobs.StartTicker(ctx, jobs.AcquisitionUpgrade, interval, planner.PlanNext,
		"interval", interval.String())
}
