package app

import (
	"altune/go-api/internal/acquisition/adapters/deezerpreview"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/shared/binpath"
	"context"
	"net/http"
	"time"

	acqPersistence "altune/go-api/internal/acquisition/adapters/persistence"
	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
)

const (
	deezerPreviewTimeout = 20 * time.Second
	deezerPreviewRate    = 1.0
)

func (a *App) upgradeLane(ledger acqPorts.TrackLedger, svc *acqService.AcquireTrackAudioService) func(*acqService.BackgroundAcquisitionScheduler) {
	store := acqPersistence.NewPgxUpgradeStore(a.pool)
	runner := upgrade.NewRunner(ledger, svc, store,
		upgrade.Config{
			DryRun: a.cfg.AcquisitionUpgradeDryRun, MaxTries: a.cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: a.cfg.AcquisitionLowBitrateFloorKbps,
		}).
		WithInPlaceCheck(svc, store, a.cfg.AcquisitionUpgradeVerifyPerHour)
	return acqService.WithUpgradeLane(runner, a.cfg.AcquisitionUpgradeConcurrency)
}

func (a *App) upgradePreviewMatcher() acqPorts.PreviewMatcher {
	printer := deezerpreview.NewFpcalcPrinter(binpath.Resolve("fpcalc", a.cfg.FFmpegLocation))
	previews := deezerpreview.New(&http.Client{Timeout: deezerPreviewTimeout}, deezerpreview.BaseURL, deezerPreviewRate, printer)
	return deezerpreview.NewMatcher(previews, printer, deezerpreview.DefaultMatchMaxBitErrorRate)
}

func (a *App) upgradeEnabled() bool { return a.pool != nil && a.cfg.AcquisitionUpgradeEnabled }

func (a *App) startAcquisitionUpgradeIfEnabled(ctx context.Context, scheduler *acqService.BackgroundAcquisitionScheduler) {
	if !a.upgradeEnabled() || scheduler == nil {
		return
	}
	a.startAcquisitionUpgrade(ctx, scheduler)
}

func (a *App) startAcquisitionUpgrade(ctx context.Context, scheduler upgrade.Scheduler) {
	planner := upgrade.NewPlanner(acqPersistence.NewPgxUpgradeStore(a.pool), scheduler, a.cfg.AcquisitionUpgradeConcurrency, a.cfg.AcquisitionUpgradeRetryAfter).CheckingUploads().
		RescuingLowBitrate(a.cfg.AcquisitionLowBitrateFloorKbps)
	interval := upgrade.Interval(a.cfg.AcquisitionUpgradePerHour)
	a.startLoggedJob(ctx, jobAcquisitionUpgrade, interval, planner.PlanNext,
		"interval", interval.String(), "dry_run", a.cfg.AcquisitionUpgradeDryRun)
}
