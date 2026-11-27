package shutdown

import (
	"altune/go-api/internal/shared/background"
	"context"
	"log/slog"
	"net/http"
	"time"
)

const (
	EventFeed      = "event feed"
	Scheduler      = "acquisition scheduler"
	Background     = "background tasks"
	LeaderElection = "leader election"
	DetachedTasks  = "detached request tasks"
)

const (
	ServerDrainTimeout   = 10 * time.Second
	backgroundDrainCap   = 15 * time.Second
	detachedDrainCap     = 10 * time.Second
	observerDrainTimeout = 2 * time.Second
	leaderReleaseTimeout = 3 * time.Second
	schedulerDrainCap    = 35 * time.Second
)

const leaderLockKeptMessage = "leadership intentionally NOT released: background drain timed out with " +
	"leader-only jobs still running; the advisory lock clears only when this " +
	"instance's DB session ends (process exit)"

type Status int

const (
	Completed Status = iota
	TimedOut
	Skipped
)

type Outcome struct {
	Component string
	Status    Status
	Budget    time.Duration
}

type Step struct {
	Component  string
	Timeout    time.Duration
	Stop       func(context.Context)
	requires   string
	blockedMsg string
}

type Components struct {
	EventFeed      func(context.Context)
	Scheduler      func(context.Context)
	Background     func(context.Context)
	LeaderElection func(context.Context)
	DetachedTasks  func(context.Context)
}

type stopper interface {
	comparable
	Shutdown(ctx context.Context)
}

func Stop[T stopper](component T) func(context.Context) {
	var absent T
	if component == absent {
		return nil
	}
	return component.Shutdown
}

type Budgets struct {
	SchedulerSeconds  int
	BackgroundSeconds int
	DetachedSeconds   int
}

func Plan(c Components, b Budgets) []Step {
	return []Step{
		{Component: EventFeed, Timeout: observerDrainTimeout, Stop: c.EventFeed},
		{Component: Scheduler, Timeout: cappedDrain(b.SchedulerSeconds, schedulerDrainCap), Stop: c.Scheduler},
		{Component: Background, Timeout: cappedDrain(b.BackgroundSeconds, backgroundDrainCap), Stop: c.Background},
		{Component: LeaderElection, Timeout: leaderReleaseTimeout, Stop: c.LeaderElection, requires: Background, blockedMsg: leaderLockKeptMessage},
		{Component: DetachedTasks, Timeout: cappedDrain(b.DetachedSeconds, detachedDrainCap), Stop: c.DetachedTasks},
	}
}

func cappedDrain(budgetSeconds int, limit time.Duration) time.Duration {
	if budgetSeconds <= 0 {
		return limit
	}
	return min(time.Duration(budgetSeconds)*time.Second, limit)
}

func Run(ctx context.Context, plan []Step) []Outcome {
	outcomes := make([]Outcome, 0, len(plan))
	completed := make(map[string]bool, len(plan))
	for _, step := range plan {
		o := runStep(ctx, step, completed)
		completed[o.Component] = o.Status == Completed
		outcomes = append(outcomes, o)
	}
	return outcomes
}

func runStep(ctx context.Context, step Step, completed map[string]bool) Outcome {
	if step.requires != "" && !completed[step.requires] {
		slog.ErrorContext(ctx, step.blockedMsg, "component", step.Component, "requires", step.requires)
		return Outcome{Component: step.Component, Status: Skipped, Budget: step.Timeout}
	}
	if step.Stop == nil {
		return Outcome{Component: step.Component, Status: Completed, Budget: step.Timeout}
	}
	return stopWithin(ctx, step.Component, step.Timeout, step.Stop)
}

func stopWithin(parent context.Context, component string, timeout time.Duration, stop func(context.Context)) Outcome {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if background.WaitWithin(ctx, func() { stop(ctx) }) {
		return Outcome{Component: component, Status: Completed, Budget: timeout}
	}
	slog.WarnContext(ctx, "component shutdown exceeded its budget",
		"component", component, "timeout", timeout.String())
	return Outcome{Component: component, Status: TimedOut, Budget: timeout}
}

func unfinished(outcomes []Outcome) []string {
	var names []string
	for _, o := range outcomes {
		if o.Status != Completed {
			names = append(names, o.Component)
		}
	}
	return names
}

func leadershipRetained(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o.Component == LeaderElection && o.Status == Skipped {
			return true
		}
	}
	return false
}

func DrainServer(parent context.Context, server *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := server.Shutdown(ctx)
	if err == nil {
		return
	}
	slog.ErrorContext(ctx, "server shutdown error", "error", err)
	if closeErr := server.Close(); closeErr != nil {
		slog.ErrorContext(ctx, "server close error", "error", closeErr)
	}
}
