package app

import (
	"altune/go-api/internal/shared/leader"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

const backgroundLockKey int64 = 8_246_113_907_441_002

// backgroundJob pairs a leader-acquired background task with a name so a panic
// it raises can be logged against the job that caused it.
type backgroundJob struct {
	name  jobName
	start func(context.Context)
}

// recoverJob runs fn and contains any panic it raises, logging the failure
// (named) and returning the recovered value (nil when fn completed normally) so
// the caller can both survive the panic and fold it into a health signal.
// Scheduled jobs run in their own goroutines outside any HTTP request, so the
// httputil.Recoverer that protects request handlers cannot catch them; without
// this an unrecovered panic in one job would terminate the whole process and
// take down live traffic.
func recoverJob(job jobName, fn func()) (recovered any) {
	defer func() {
		if r := recover(); r != nil {
			recovered = r
			slog.Error("background job panicked; recovered",
				"job", job, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	fn()
	return nil
}

// guard is the fire-and-forget form of recoverJob used by the leader-acquire
// path, which has no per-run health signal to update.
func guard(job jobName, fn func()) { _ = recoverJob(job, fn) }

// whenLeader registers start to run once the first leadership term begins. It
// is called with the app-lifetime context and never called again, so leadership
// is a precondition of the start, not of the work: a job that keeps its own
// loop (the alert monitor, the eval meter) must scope each pass with
// leaderContext, or it will still be running after the term it started in ended.
func (a *App) whenLeader(name jobName, start func(context.Context)) {
	a.backgroundStarts = append(a.backgroundStarts, backgroundJob{name: name, start: start})
}

func (a *App) startBackgroundWhenLeader(ctx context.Context) {
	a.election = leader.NewElection(a.pool, backgroundLockKey)
	a.election.Start(ctx)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if !a.election.Await(ctx) {
			return
		}
		for _, job := range a.backgroundStarts {
			guard(job.name, func() { job.start(ctx) })
		}
	}()
}

// startTicker registers the job's control block immediately, before leadership
// is acquired, so every instance (leader or follower) lists the job and an
// operator's kill-switch flip on a follower already holds if it takes over.
func (a *App) startTicker(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	a.job(name)
	a.whenLeader(name, func(ctx context.Context) { a.runTicker(ctx, name, interval, fn) })
}

// jobRunBudgetFraction sizes each job run's deadline as a fraction of its tick
// interval (budget = interval / jobRunBudgetFraction), so a run that hangs on a
// dependency is cut off well before the next tick is due.
const jobRunBudgetFraction = 2

// errJobRunBudgetExceeded is the cancellation cause of a job run that outlived
// its per-invocation budget, distinguishing it from leadership loss or shutdown.
var errJobRunBudgetExceeded = errors.New("background job run exceeded its budget")

// jobRunBudget is the per-invocation deadline for a job ticking every interval.
func jobRunBudget(interval time.Duration) time.Duration {
	return interval / jobRunBudgetFraction
}

func (a *App) runTicker(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	jc := a.job(name)
	budget := jobRunBudget(interval)
	a.loopEvery(ctx, interval, func() { a.tick(ctx, jc, name, budget, fn) })
}

func (a *App) startEveryInstanceTicker(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	jc := a.job(name)
	budget := jobRunBudget(interval)
	a.loopEvery(ctx, interval, func() { a.tickEveryInstance(ctx, jc, name, budget, fn) })
}

func (a *App) loopEvery(ctx context.Context, interval time.Duration, run func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

func (a *App) tickEveryInstance(ctx context.Context, jc *jobControl, name jobName, budget time.Duration, fn func(context.Context) error) {
	if jc.disabled.Load() {
		jc.skipped.Add(1)
		return
	}
	a.runJob(ctx, jc, name, budget, fn)
}

// tick runs one scheduled invocation, honouring the kill switch, re-checking
// leadership (a blue-green deploy can hand the lock to another instance after a
// job first started, and a job that kept firing would duplicate the new
// leader's work), containing any panic, and recording the outcome as the job's
// health signal. A recovered panic counts as a failed run.
//
// fn runs under a context scoped to the current leadership term: if this
// instance loses leadership mid-run (e.g. its DB session dies), the context is
// canceled with leader.ErrLeadershipLost so the stale run is cut off before a
// successor starts the same job, instead of racing the successor's writes.
//
// The run is also bounded by budget, derived from the leadership-term context:
// the ticker loop calls tick synchronously, so without a deadline one dependency
// call that never returns (lock contention, a partition to Postgres) would stop
// the job for good until restart. On expiry the context is canceled with
// errJobRunBudgetExceeded and the run's error is recorded as a failure, so the
// next tick fires on schedule. A job must honour ctx for this to take effect.
func (a *App) tick(ctx context.Context, jc *jobControl, name jobName, budget time.Duration, fn func(context.Context) error) {
	if jc.disabled.Load() {
		jc.skipped.Add(1)
		return
	}
	leaderCtx, release, ok := a.leaderContext(ctx)
	if !ok {
		return
	}
	defer release()
	a.runJob(leaderCtx, jc, name, budget, fn)
}

func (a *App) runJob(parent context.Context, jc *jobControl, name jobName, budget time.Duration, fn func(context.Context) error) {
	jobCtx, cancel := context.WithTimeoutCause(parent, budget, errJobRunBudgetExceeded)
	defer cancel()
	var err error
	if r := recoverJob(name, func() { err = fn(jobCtx) }); r != nil {
		err = fmt.Errorf("panic: %v", r)
	}
	if err != nil && errors.Is(context.Cause(jobCtx), errJobRunBudgetExceeded) {
		slog.Warn("background job run exceeded its budget; canceled",
			"job", name, "budget", budget.String(), "error", err)
	}
	jc.record(err)
}

// leaderContext scopes one leader-only pass — a ticker run, or a pass of a job
// that owns its loop — to the current leadership term; ok is false when this
// instance is not leader. With no election configured (unit tests of the ticker
// mechanics, single-process setups) the pass is always allowed under a plain
// child of ctx, matching the pre-election behaviour.
func (a *App) leaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if a.election == nil {
		jobCtx, cancel := context.WithCancel(ctx)
		return jobCtx, cancel, true
	}
	return a.election.LeaderContext(ctx)
}
