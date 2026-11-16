package app

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/shared/binpath"
	"context"
	"net/http"
	"time"
)

const (
	deezerPreviewTimeout = 20 * time.Second
	deezerPreviewRate    = 1.0
)

func (a *App) upgradeLane(ledger acquisition.TrackLedgerPort, svc *acquisition.AcquireTrackAudioService) func(*acquisition.BackgroundAcquisitionScheduler) {
	store := acquisition.NewPgxUpgradeStore(a.pool)
	runner := acquisition.NewUpgradeRunner(ledger, svc, store,
		acquisition.UpgradeConfig{
			DryRun: a.cfg.AcquisitionUpgradeDryRun, MaxTries: a.cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: a.cfg.AcquisitionLowBitrateFloorKbps,
		}).
		WithInPlaceCheck(svc, store, a.cfg.AcquisitionUpgradeVerifyPerHour)
	return acquisition.WithUpgradeLane(runner, a.cfg.AcquisitionUpgradeConcurrency)
}

func (a *App) upgradePreviewMatcher() acquisition.PreviewMatcher {
	printer := acquisition.NewFpcalcPrinter(binpath.Resolve("fpcalc", a.cfg.FFmpegLocation))
	previews := acquisition.NewDeezerPreview(&http.Client{Timeout: deezerPreviewTimeout}, acquisition.DeezerPreviewBaseURL, deezerPreviewRate, printer)
	return acquisition.NewPreviewMatcher(previews, printer, acquisition.DefaultMatchMaxBitErrorRate)
}

func (a *App) upgradeEnabled() bool { return a.pool != nil && a.cfg.AcquisitionUpgradeEnabled }

func (a *App) startAcquisitionUpgradeIfEnabled(ctx context.Context, scheduler *acquisition.BackgroundAcquisitionScheduler) {
	if !a.upgradeEnabled() || scheduler == nil {
		return
	}
	a.startAcquisitionUpgrade(ctx, scheduler)
}

func (a *App) startAcquisitionUpgrade(ctx context.Context, scheduler acquisition.UpgradeScheduler) {
	planner := acquisition.NewUpgradePlanner(acquisition.NewPgxUpgradeStore(a.pool), scheduler, a.cfg.AcquisitionUpgradeConcurrency, a.cfg.AcquisitionUpgradeRetryAfter).CheckingUploads().
		RescuingLowBitrate(a.cfg.AcquisitionLowBitrateFloorKbps)
	interval := acquisition.UpgradeInterval(a.cfg.AcquisitionUpgradePerHour)
	a.startLoggedJob(ctx, jobAcquisitionUpgrade, interval, planner.PlanNext,
		"interval", interval.String(), "dry_run", a.cfg.AcquisitionUpgradeDryRun)
}
