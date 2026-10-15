package app

import (
	"altune/go-api/internal/acquisition/service/upgrade"
	"context"

	acqPersistence "altune/go-api/internal/acquisition/adapters/persistence"
	acqPorts "altune/go-api/internal/acquisition/ports"
	acqService "altune/go-api/internal/acquisition/service"
)

func (a *App) upgradeLane(ledger acqPorts.TrackLedger, replacer upgrade.Replacer) func(*acqService.BackgroundAcquisitionScheduler) {
	runner := upgrade.NewRunner(ledger, replacer, acqPersistence.NewPgxUpgradeStore(a.pool),
		upgrade.Config{DryRun: a.cfg.AcquisitionUpgradeDryRun, MaxTries: a.cfg.AcquisitionUpgradeMaxTries})
	return acqService.WithUpgradeLane(runner, a.cfg.AcquisitionUpgradeConcurrency)
}

func (a *App) upgradeEnabled() bool { return a.pool != nil && a.cfg.AcquisitionUpgradeEnabled }

func (a *App) startAcquisitionUpgradeIfEnabled(ctx context.Context, scheduler *acqService.BackgroundAcquisitionScheduler) {
	if !a.upgradeEnabled() || scheduler == nil {
		return
	}
	a.startAcquisitionUpgrade(ctx, scheduler)
}

func (a *App) startAcquisitionUpgrade(ctx context.Context, scheduler upgrade.Scheduler) {
	planner := upgrade.NewPlanner(acqPersistence.NewPgxUpgradeStore(a.pool), scheduler, a.cfg.AcquisitionUpgradeConcurrency, a.cfg.AcquisitionUpgradeRetryAfter)
	interval := upgrade.Interval(a.cfg.AcquisitionUpgradePerHour)
	a.startLoggedJob(ctx, jobAcquisitionUpgrade, interval, planner.PlanNext,
		"interval", interval.String(), "dry_run", a.cfg.AcquisitionUpgradeDryRun)
}
