package app

import (
	"altune/go-api/internal/app/shutdown"
	"context"
)

func (a *application) shutdownComponents() shutdown.Components {
	return shutdown.Components{
		AlertMonitor:   shutdown.Stop(a.alertMonitor),
		EventFeed:      shutdown.Stop(a.eventFeed),
		EvalMeter:      shutdown.Stop(a.evalMeter),
		Scheduler:      shutdown.Stop(a.scheduler),
		Background:     func(context.Context) { a.jobs.Wait() },
		LeaderElection: shutdown.Stop(a.jobs.Election()),
		DetachedTasks:  func(ctx context.Context) { a.background.Drain(ctx) },
	}
}

func (a *application) runShutdownSequence(ctx context.Context) []shutdown.Outcome {
	return shutdown.Run(ctx, shutdown.Plan(a.shutdownComponents(), shutdown.Budgets{
		SchedulerSeconds:  a.cfg.AcquisitionDrainBudgetSeconds,
		BackgroundSeconds: a.cfg.BackgroundDrainBudgetSeconds,
		DetachedSeconds:   a.cfg.DetachedDrainBudgetSeconds,
	}))
}
