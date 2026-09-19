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

// shutdownOutcome records whether one component's bounded shutdown finished
// within its budget. A component that timed out is presumed still running when
// cleanup() closes the DB pool and Redis client, so the distinction must be
// surfaced rather than swallowed. skipped marks a component whose shutdown was
// deliberately never attempted because a prerequisite did not complete.
type shutdownOutcome struct {
	name      string
	completed bool
	skipped   bool
}

// leadershipRetained reports whether the leader-election release was skipped,
// meaning this instance still holds the advisory lock on a pooled connection.
func leadershipRetained(outcomes []shutdownOutcome) bool {
	for _, o := range outcomes {
		if o.name == leaderElectionComponent && o.skipped {
			return true
		}
	}
	return false
}

// unfinishedShutdowns returns the names of components that did not complete
// shutdown within their budget, in declaration order.
func unfinishedShutdowns(outcomes []shutdownOutcome) []string {
	var names []string
	for _, o := range outcomes {
		if !o.completed {
			names = append(names, o.name)
		}
	}
	return names
}

// shutdownComponent runs fn with a bounded context and reports whether it
// returned before the budget elapsed. fn runs on its own goroutine so a
// component that ignores the deadline cannot wedge the whole shutdown sequence;
// a timeout is surfaced as an outcome (and logged) instead of silently falling
// through to cleanup().
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

// componentShutdown is one row of the ordered shutdown table: a named component
// with its own timeout budget and a nil-checked shutdown. Collapsing the
// previously copy-pasted blocks into a table means a newly added shutdownable
// field is a single row that cannot skip the nil-check or the bounded,
// outcome-reporting shutdownComponent path. requires, when set, names an
// earlier row that must have completed for this row to run at all; blockedMsg
// is the error logged when it did not.
type componentShutdown struct {
	name       string
	timeout    time.Duration
	shutdown   func(context.Context)
	requires   string
	blockedMsg string
}

// shutdownPlan is the ordered shutdown table. Every row, the two wait-group
// drains included, runs through the single bounded shutdownComponent path.
// The background drain MUST run before the leader-election lock is released:
// releasing first would let the next instance win leadership and start its own
// copies while these are still mid-flight (e.g. the corpus refresh's blocking
// Materialize), running the same leader-only job twice. Ordering alone is not
// enough: a drain that times out leaves those jobs running, so the release row
// requires the drain to have completed and is skipped otherwise.
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
		{name: "acquisition scheduler", timeout: 30 * time.Second, shutdown: func(ctx context.Context) {
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

// runShutdownSequence shuts every shutdownPlan component down in strict order
// and collects each outcome.
func (a *App) runShutdownSequence() []shutdownOutcome {
	return a.runShutdownPlan(a.shutdownPlan())
}

// runShutdownPlan runs plan rows in order, gating each on its requires row.
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

// runPlannedShutdown runs one plan row, or skips it (logging blockedMsg) when
// the row it requires did not complete.
func (a *App) runPlannedShutdown(c componentShutdown, completed map[string]bool) shutdownOutcome {
	if c.requires != "" && !completed[c.requires] {
		slog.Error(c.blockedMsg, "component", c.name, "requires", c.requires)
		return shutdownOutcome{name: c.name, skipped: true}
	}
	return a.shutdownComponent(c.name, c.timeout, c.shutdown)
}

// drainBackground waits, bounded by timeout, for the leader-gated background
// goroutines tracked in a.wg.
func (a *App) drainBackground(timeout time.Duration) shutdownOutcome {
	return a.shutdownComponent(backgroundTasksComponent, timeout, a.waitBackground)
}

// drainSearchBackground waits, bounded by timeout, for the discovery search
// service's own detached background work (identity-bridge persistence,
// telemetry emit, vocab ingest, all on context.WithoutCancel) to finish before
// cleanup() closes the DB pool and Redis client out from under it.
func (a *App) drainSearchBackground(timeout time.Duration) shutdownOutcome {
	return a.shutdownComponent(discoverySearchComponent, timeout, a.waitSearchBackground)
}

// waitBackground blocks until every leader-gated background goroutine exits.
// It ignores ctx: shutdownComponent bounds the wait.
func (a *App) waitBackground(context.Context) { a.wg.Wait() }

// waitSearchBackground blocks until the search service's detached work exits;
// with no wired service there is nothing to wait for.
func (a *App) waitSearchBackground(context.Context) {
	if a.searchSvc != nil {
		a.searchSvc.WaitForBackground()
	}
}
