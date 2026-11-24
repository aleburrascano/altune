package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/app/jobs"
	"context"
)

func (a *App) startAcquisitionUpgrade(ctx context.Context, scheduler *acquisition.BackgroundAcquisitionScheduler) {
	if a.pool == nil || scheduler == nil {
		return
	}
	planner := acquisition.NewUpgradePlanner(acquisition.NewPgxUpgradeStore(a.pool), scheduler, a.cfg.AcquisitionUpgradeConcurrency, a.cfg.AcquisitionUpgradeRetryAfter).CheckingUploads().
		RescuingLowBitrate(a.cfg.AcquisitionLowBitrateFloorKbps)
	interval := acquisition.UpgradeInterval(a.cfg.AcquisitionUpgradePerHour)
	a.jobs.StartTicker(ctx, jobs.AcquisitionUpgrade, interval, planner.PlanNext,
		"interval", interval.String())
}
