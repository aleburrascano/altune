package shutdown

import (
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"net/http"
	"time"
)

const (
	AlertMonitor   = "alert monitor"
	EventFeed      = "event feed"
	EvalMeter      = "eval meter"
	Scheduler      = "acquisition scheduler"
	Background     = "background tasks"
	LeaderElection = "leader election"
	DetachedTasks  = "detached request tasks"
)

const (
	ServerDrainTimeout     = 10 * time.Second
	backgroundDrainTimeout = 15 * time.Second
	detachedDrainTimeout   = 10 * time.Second
	observerDrainTimeout   = 2 * time.Second
	leaderReleaseTimeout   = 3 * time.Second
	schedulerDrainCap      = 35 * time.Second
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
}

type Step struct {
	Component  string
	Timeout    time.Duration
	Stop       func(context.Context)
	requires   string
	blockedMsg string
}

type Components struct {
	AlertMonitor   func(context.Context)
	EventFeed      func(context.Context)
	EvalMeter      func(context.Context)
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

func Plan(c Components, schedulerDrainBudgetSeconds int) []Step {
	return []Step{
		{Component: AlertMonitor, Timeout: observerDrainTimeout, Stop: c.AlertMonitor},
		{Component: EventFeed, Timeout: observerDrainTimeout, Stop: c.EventFeed},
		{Component: EvalMeter, Timeout: observerDrainTimeout, Stop: c.EvalMeter},
		{Component: Scheduler, Timeout: schedulerDrainTimeout(schedulerDrainBudgetSeconds), Stop: c.Scheduler},
		{Component: Background, Timeout: backgroundDrainTimeout, Stop: c.Background},
		{Component: LeaderElection, Timeout: leaderReleaseTimeout, Stop: c.LeaderElection, requires: Background, blockedMsg: leaderLockKeptMessage},
		{Component: DetachedTasks, Timeout: detachedDrainTimeout, Stop: c.DetachedTasks},
	}
}

func schedulerDrainTimeout(budgetSeconds int) time.Duration {
	if budgetSeconds <= 0 {
		return schedulerDrainCap
	}
	return min(time.Duration(budgetSeconds)*time.Second, schedulerDrainCap)
}

func Run(plan []Step) []Outcome {
	ctx := context.Background()
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
		return Outcome{Component: step.Component, Status: Skipped}
	}
	if step.Stop == nil {
		return Outcome{Component: step.Component, Status: Completed}
	}
	return stopWithin(step.Component, step.Timeout, step.Stop)
}

func stopWithin(component string, timeout time.Duration, stop func(context.Context)) Outcome {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if runloop.WaitWithin(ctx, func() { stop(ctx) }) {
		return Outcome{Component: component, Status: Completed}
	}
	slog.WarnContext(ctx, "component shutdown exceeded its budget",
		"component", component, "timeout", timeout.String())
	return Outcome{Component: component, Status: TimedOut}
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

func DrainServer(server *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
