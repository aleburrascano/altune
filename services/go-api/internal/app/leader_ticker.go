package app

import (
	"altune/go-api/internal/shared/leader"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync/atomic"
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

// jobName identifies a background job. It keys the App's job registry and is
// the name an operator passes to the admin job switchboard, so every job is
// declared once here rather than spelled as a literal at its registration site.
// The string values are wire identifiers (GET /admin/jobs lists them, POST
// /admin/jobs/{name}/enable|disable matches on them): renaming one breaks
// operator tooling.
type jobName string

const (
	jobEvalMeter                jobName = "eval meter"
	jobAlertMonitor             jobName = "alert monitor"
	jobStalePendingReconcile    jobName = "stale pending reconcile"
	jobOrphanedAudioReconcile   jobName = "orphaned audio reconcile"
	jobBehavioralCorpusRefresh  jobName = "behavioral corpus refresh"
	jobDiscoveryMetricsRollup   jobName = "discovery metrics rollup"
	jobDiscographyEventPrune    jobName = "discography event prune"
	jobVocabularyRefresh        jobName = "vocabulary refresh"
	jobBehavioralRankingRefresh jobName = "behavioral ranking refresh"
	// jobStreamRecovery is not a ticker: it is the request-path recovery that
	// marks a track failed and reschedules its acquisition when a stream finds
	// its audio missing. It shares the job registry so operators flip it through
	// the same /admin/jobs switchboard.
	jobStreamRecovery jobName = "stream recovery"
)

// jobControl carries the runtime kill switch and the health signal for one
// background job. Every field is touched concurrently: the ticker goroutine
// records outcomes while an operator toggles the switch and reads health at
// runtime, so all access goes through atomics.
type jobControl struct {
	disabled    atomic.Bool
	failures    atomic.Int64
	skipped     atomic.Int64 // ticks that returned early because the kill switch was off
	lastSuccess atomic.Int64 // unix nanoseconds of the last successful run; 0 = never
	lastFailure atomic.Int64 // unix nanoseconds of the last failed run; 0 = never
}

// record folds one run's outcome into the job's health signal.
func (jc *jobControl) record(err error) {
	now := time.Now().UnixNano()
	if err != nil {
		jc.failures.Add(1)
		jc.lastFailure.Store(now)
		return
	}
	jc.lastSuccess.Store(now)
}

// JobHealth is the queryable snapshot of one background job's kill switch and
// last-success/failure signal, exposed so an operator (or a health probe) can
// tell whether an unattended job is still doing its work.
type JobHealth struct {
	Name        string
	Enabled     bool
	Failures    int64
	Skipped     int64     // ticks skipped by the kill switch
	LastSuccess time.Time // zero when the job has never succeeded
	LastFailure time.Time // zero when the job has never failed
}

// job returns the control block for name, creating it on first use so a job's
// health is queryable from the moment it is registered.
func (a *App) job(name jobName) *jobControl {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	if a.jobs == nil {
		a.jobs = make(map[jobName]*jobControl)
	}
	jc, ok := a.jobs[name]
	if !ok {
		jc = &jobControl{}
		a.jobs[name] = jc
	}
	return jc
}

// jobSwitch registers name and returns its kill-switch check for work that runs
// outside a ticker. Each call reports whether the job is enabled, counting a
// disabled call as skipped so GET /admin/jobs shows the suppressed work.
func (a *App) jobSwitch(name jobName) func() bool {
	jc := a.job(name)
	return func() bool {
		if jc.disabled.Load() {
			jc.skipped.Add(1)
			return false
		}
		return true
	}
}

// SetJobEnabled flips a registered background job's kill switch at runtime and
// returns the job's resulting health snapshot. A disabled job stays registered
// and keeps ticking, but each tick returns early without doing work, so an
// operator can stop a misbehaving job without a redeploy. An unknown name
// reports ok=false and registers nothing, so a mistyped name cannot mint a
// phantom job that appears in JobHealth.
func (a *App) SetJobEnabled(name jobName, enabled bool) (JobHealth, bool) {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	jc, ok := a.jobs[name]
	if !ok {
		return JobHealth{}, false
	}
	jc.disabled.Store(!enabled)
	return jc.snapshot(name), true
}

// JobHealth returns a snapshot of every registered background job's health,
// ordered by name for stable output.
func (a *App) JobHealth() []JobHealth {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	out := make([]JobHealth, 0, len(a.jobs))
	for name, jc := range a.jobs {
		out = append(out, jc.snapshot(name))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (jc *jobControl) snapshot(name jobName) JobHealth {
	return JobHealth{
		Name:        string(name),
		Enabled:     !jc.disabled.Load(),
		Failures:    jc.failures.Load(),
		Skipped:     jc.skipped.Load(),
		LastSuccess: nanosToTime(jc.lastSuccess.Load()),
		LastFailure: nanosToTime(jc.lastFailure.Load()),
	}
}

// nanosToTime maps a stored unix-nano timestamp back to a time.Time, keeping the
// "never happened" sentinel (0) as the zero time rather than the unix epoch.
func nanosToTime(nanos int64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos).UTC()
}

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

const (
	backgroundTasksComponent = "background tasks"
	leaderElectionComponent  = "leader election"
	discoverySearchComponent = "discovery search"
	backgroundDrainTimeout   = 30 * time.Second
)

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
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.tick(ctx, jc, name, budget, fn)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.tick(ctx, jc, name, budget, fn)
			}
		}
	}()
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
	jobCtx, cancel := context.WithTimeoutCause(leaderCtx, budget, errJobRunBudgetExceeded)
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

// leaderContext scopes one job run to the current leadership term; ok is false
// when this instance is not leader. With no election configured (unit tests of
// the ticker mechanics, single-process setups) the run is always allowed under
// a plain child of ctx, matching the pre-election behaviour.
func (a *App) leaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if a.election == nil {
		jobCtx, cancel := context.WithCancel(ctx)
		return jobCtx, cancel, true
	}
	return a.election.LeaderContext(ctx)
}
