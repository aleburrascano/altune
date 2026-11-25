package app

import (
	"altune/go-api/internal/app/shutdown"
	"context"
)

func (a *App) shutdownComponents() shutdown.Components {
	return shutdown.Components{
		AlertMonitor:    shutdown.Stop(a.alertMonitor),
		EventFeed:       shutdown.Stop(a.eventFeed),
		EvalMeter:       shutdown.Stop(a.evalMeter),
		Scheduler:       shutdown.Stop(a.scheduler),
		Background:      func(context.Context) { a.jobs.Background().Wait() },
		LeaderElection:  shutdown.Stop(a.jobs.Election()),
		DiscoverySearch: func(ctx context.Context) { a.background.Drain(ctx) },
	}
}

func (a *App) runShutdownSequence() []shutdown.Outcome {
	return shutdown.Run(shutdown.Plan(a.shutdownComponents(), a.cfg.AcquisitionDrainBudgetSeconds))
}
