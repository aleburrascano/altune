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
	backgroundDrainTimeout   = 30 * time.Second
)

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
		{name: "alert monitor", timeout: 5 * time.Second, shutdown: func(ctx context.Context) {
			if a.alertMonitor != nil {
				a.alertMonitor.Shutdown(ctx)
			}
		}},
		{name: "event feed", timeout: 5 * time.Second, shutdown: func(ctx context.Context) {
			if a.eventFeed != nil {
				a.eventFeed.Shutdown(ctx)
			}
		}},
		{name: "eval meter", timeout: 5 * time.Second, shutdown: func(ctx context.Context) {
			if a.evalMeter != nil {
				a.evalMeter.Shutdown(ctx)
			}
		}},
		{name: "acquisition scheduler", timeout: 70 * time.Second, shutdown: func(ctx context.Context) {
			if a.scheduler != nil {
				a.scheduler.Shutdown(ctx)
			}
		}},
		{name: backgroundTasksComponent, timeout: backgroundDrainTimeout, shutdown: a.waitBackground},
		{
			name: leaderElectionComponent, timeout: 5 * time.Second,
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
		{name: discoverySearchComponent, timeout: backgroundDrainTimeout, shutdown: a.waitSearchBackground},
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
