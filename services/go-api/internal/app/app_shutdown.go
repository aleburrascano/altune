package app

import (
	"context"
	"log/slog"
	"time"
)

const (
	backgroundTasksComponent = "background tasks"
	leaderElectionComponent  = "leader election"
	discoverySearchComponent = "discovery search"
	backgroundDrainTimeout   = 15 * time.Second
	discoveryDrainTimeout    = 10 * time.Second
	serverDrainTimeout       = 10 * time.Second
	observerDrainTimeout     = 2 * time.Second
	leaderReleaseTimeout     = 3 * time.Second
	schedulerDrainCap        = 35 * time.Second

	shutdownTotalBudget = 80 * time.Second
)

func (a *App) schedulerDrainTimeout() time.Duration {
	if a.cfg == nil || a.cfg.AcquisitionDrainBudgetSeconds <= 0 {
		return schedulerDrainCap
	}
	return min(time.Duration(a.cfg.AcquisitionDrainBudgetSeconds)*time.Second, schedulerDrainCap)
}

func (a *App) drainServer(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := a.server.Shutdown(ctx)
	if err == nil {
		return
	}
	slog.Error("server shutdown error", "error", err)
	if closeErr := a.server.Close(); closeErr != nil {
		slog.Error("server close error", "error", closeErr)
	}
}

type shutdownOutcome struct {
	name      string
	completed bool
	skipped   bool
}

func leadershipRetained(outcomes []shutdownOutcome) bool {
	for _, o := range outcomes {
		if o.name == leaderElectionComponent && o.skipped {
			return true
		}
	}
	return false
}

func unfinishedShutdowns(outcomes []shutdownOutcome) []string {
	var names []string
	for _, o := range outcomes {
		if !o.completed {
			names = append(names, o.name)
		}
	}
	return names
}

func (a *App) shutdownComponent(name string, timeout time.Duration, fn func(context.Context)) shutdownOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	select {
	case <-done:
		return shutdownOutcome{name: name, completed: true}
	case <-ctx.Done():
		slog.Warn("component shutdown exceeded its budget",
			"component", name, "timeout", timeout.String())
		return shutdownOutcome{name: name, completed: false}
	}
}

type componentShutdown struct {
	name       string
	timeout    time.Duration
	shutdown   func(context.Context)
	requires   string
	blockedMsg string
}

func (a *App) shutdownPlan() []componentShutdown {
	return []componentShutdown{
		{name: "alert monitor", timeout: observerDrainTimeout, shutdown: func(ctx context.Context) {
			if a.alertMonitor != nil {
				a.alertMonitor.Shutdown(ctx)
			}
		}},
		{name: "event feed", timeout: observerDrainTimeout, shutdown: func(ctx context.Context) {
			if a.eventFeed != nil {
				a.eventFeed.Shutdown(ctx)
			}
		}},
		{name: "eval meter", timeout: observerDrainTimeout, shutdown: func(ctx context.Context) {
			if a.evalMeter != nil {
				a.evalMeter.Shutdown(ctx)
			}
		}},
		{name: "acquisition scheduler", timeout: a.schedulerDrainTimeout(), shutdown: func(ctx context.Context) {
			if a.scheduler != nil {
				a.scheduler.Shutdown(ctx)
			}
		}},
		{name: backgroundTasksComponent, timeout: backgroundDrainTimeout, shutdown: a.waitBackground},
		{
			name: leaderElectionComponent, timeout: leaderReleaseTimeout,
			shutdown: func(ctx context.Context) {
				if a.election != nil {
					a.election.Shutdown(ctx)
				}
			},
			requires: backgroundTasksComponent,
			blockedMsg: "leadership intentionally NOT released: background drain timed out with " +
				"leader-only jobs still running; the advisory lock clears only when this " +
				"instance's DB session ends (process exit)",
		},
		{name: discoverySearchComponent, timeout: discoveryDrainTimeout, shutdown: a.waitSearchBackground},
	}
}

func (a *App) runShutdownSequence() []shutdownOutcome {
	return a.runShutdownPlan(a.shutdownPlan())
}

func (a *App) runShutdownPlan(plan []componentShutdown) []shutdownOutcome {
	outcomes := make([]shutdownOutcome, 0, len(plan))
	completed := make(map[string]bool, len(plan))
	for _, c := range plan {
		o := a.runPlannedShutdown(c, completed)
		completed[o.name] = o.completed
		outcomes = append(outcomes, o)
	}
	return outcomes
}

func (a *App) runPlannedShutdown(c componentShutdown, completed map[string]bool) shutdownOutcome {
	if c.requires != "" && !completed[c.requires] {
		slog.Error(c.blockedMsg, "component", c.name, "requires", c.requires)
		return shutdownOutcome{name: c.name, skipped: true}
	}
	return a.shutdownComponent(c.name, c.timeout, c.shutdown)
}

func (a *App) waitBackground(context.Context) { a.wg.Wait() }

func (a *App) waitSearchBackground(context.Context) {
	if a.searchSvc != nil {
		a.searchSvc.WaitForBackground()
	}
}
