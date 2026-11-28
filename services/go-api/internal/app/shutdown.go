package app

import (
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const (
	stopEventFeed      = "event feed"
	stopScheduler      = "acquisition scheduler"
	stopBackground     = "background tasks"
	stopLeaderElection = "leader election"
	stopDetachedTasks  = "detached request tasks"
)

const (
	serverDrainTimeout   = 10 * time.Second
	backgroundDrainCap   = 15 * time.Second
	detachedDrainCap     = 10 * time.Second
	observerDrainTimeout = 2 * time.Second
	leaderReleaseTimeout = 3 * time.Second
	schedulerDrainCap    = 35 * time.Second
)

const leaderLockKeptMessage = "leadership intentionally NOT released: background drain timed out with " +
	"leader-only jobs still running; the advisory lock clears only when this " +
	"instance's DB session ends (process exit)"

type stopStatus int

const (
	stopCompleted stopStatus = iota
	stopTimedOut
	stopSkipped
)

type stopOutcome struct {
	Component string
	Status    stopStatus
	Budget    time.Duration
}

type stopStep struct {
	Component  string
	Timeout    time.Duration
	Stop       func(context.Context)
	requires   string
	blockedMsg string
}

type stopComponents struct {
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

func stopOf[T stopper](component T) func(context.Context) {
	var absent T
	if component == absent {
		return nil
	}
	return component.Shutdown
}

type stopBudgets struct {
	SchedulerSeconds  int
	BackgroundSeconds int
	DetachedSeconds   int
}

func planShutdown(c stopComponents, b stopBudgets) []stopStep {
	return []stopStep{
		{Component: stopEventFeed, Timeout: observerDrainTimeout, Stop: c.EventFeed},
		{Component: stopScheduler, Timeout: cappedDrain(b.SchedulerSeconds, schedulerDrainCap), Stop: c.Scheduler},
		{Component: stopBackground, Timeout: cappedDrain(b.BackgroundSeconds, backgroundDrainCap), Stop: c.Background},
		{Component: stopLeaderElection, Timeout: leaderReleaseTimeout, Stop: c.LeaderElection, requires: stopBackground, blockedMsg: leaderLockKeptMessage},
		{Component: stopDetachedTasks, Timeout: cappedDrain(b.DetachedSeconds, detachedDrainCap), Stop: c.DetachedTasks},
	}
}

func cappedDrain(budgetSeconds int, limit time.Duration) time.Duration {
	if budgetSeconds <= 0 {
		return limit
	}
	return min(time.Duration(budgetSeconds)*time.Second, limit)
}

func runShutdown(ctx context.Context, plan []stopStep) []stopOutcome {
	outcomes := make([]stopOutcome, 0, len(plan))
	completed := make(map[string]bool, len(plan))
	for _, step := range plan {
		o := runStep(ctx, step, completed)
		completed[o.Component] = o.Status == stopCompleted
		outcomes = append(outcomes, o)
	}
	return outcomes
}

func runStep(ctx context.Context, step stopStep, completed map[string]bool) stopOutcome {
	if step.requires != "" && !completed[step.requires] {
		slog.ErrorContext(ctx, step.blockedMsg, "component", step.Component, "requires", step.requires)
		return stopOutcome{Component: step.Component, Status: stopSkipped, Budget: step.Timeout}
	}
	if step.Stop == nil {
		return stopOutcome{Component: step.Component, Status: stopCompleted, Budget: step.Timeout}
	}
	return stopWithin(ctx, step.Component, step.Timeout, step.Stop)
}

func stopWithin(parent context.Context, component string, timeout time.Duration, stop func(context.Context)) stopOutcome {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if background.WaitWithin(ctx, func() { stop(ctx) }) {
		return stopOutcome{Component: component, Status: stopCompleted, Budget: timeout}
	}
	slog.WarnContext(ctx, "component shutdown exceeded its budget",
		"component", component, "timeout", timeout.String())
	return stopOutcome{Component: component, Status: stopTimedOut, Budget: timeout}
}

func unfinished(outcomes []stopOutcome) []string {
	var names []string
	for _, o := range outcomes {
		if o.Status != stopCompleted {
			names = append(names, o.Component)
		}
	}
	return names
}

func leadershipRetained(outcomes []stopOutcome) bool {
	for _, o := range outcomes {
		if o.Component == stopLeaderElection && o.Status == stopSkipped {
			return true
		}
	}
	return false
}

func drainServer(parent context.Context, server *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := server.Shutdown(ctx)
	if err == nil {
		return
	}
	slog.ErrorContext(ctx, "server shutdown error", logging.KeyError, err)
	if closeErr := server.Close(); closeErr != nil {
		slog.ErrorContext(ctx, "server close error", logging.KeyError, closeErr)
	}
}

type datastores struct {
	Pool  *pgxpool.Pool
	Redis *goredis.Client
}

func closeDatastores(ctx context.Context, outcomes []stopOutcome, ds datastores) {
	if unstopped := unfinished(outcomes); len(unstopped) > 0 {
		slog.WarnContext(ctx, "closing DB/Redis while components are still shutting down",
			"components", strings.Join(unstopped, ", "))
	}
	switch {
	case leadershipRetained(outcomes):
		slog.ErrorContext(ctx, "leaving DB pool open so process exit releases the retained leader lock")
	case ds.Pool != nil:
		ds.Pool.Close()
	}
	closeRedis(ctx, ds.Redis)
}

func closeRedis(ctx context.Context, client *goredis.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		slog.ErrorContext(ctx, "redis client close error", logging.KeyError, err)
	}
}

func (a *application) shutdownComponents() stopComponents {
	return stopComponents{
		EventFeed:      stopOf(a.eventFeed),
		Scheduler:      stopOf(a.download.Scheduler()),
		Background:     func(context.Context) { a.jobs.Wait() },
		LeaderElection: stopOf(a.jobs.Election()),
		DetachedTasks:  func(ctx context.Context) { a.background.Drain(ctx) },
	}
}

func (a *application) runShutdownSequence(ctx context.Context) []stopOutcome {
	return runShutdown(ctx, planShutdown(a.shutdownComponents(), stopBudgets{
		SchedulerSeconds:  a.cfg.AcquisitionDrainBudgetSeconds,
		BackgroundSeconds: a.cfg.BackgroundDrainBudgetSeconds,
		DetachedSeconds:   a.cfg.DetachedDrainBudgetSeconds,
	}))
}
