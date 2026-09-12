package app

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"sync/atomic"
	"time"

	"altune/go-api/internal/shared/leader"
)

const backgroundLockKey int64 = 8_246_113_907_441_002

// backgroundJob pairs a leader-acquired background task with a name so a panic
// it raises can be logged against the job that caused it.
type backgroundJob struct {
	name  string
	start func(context.Context)
}

// recoverJob runs fn and contains any panic it raises, logging the failure
// (named) and returning the recovered value (nil when fn completed normally) so
// the caller can both survive the panic and fold it into a health signal.
// Scheduled jobs run in their own goroutines outside any HTTP request, so the
// httputil.Recoverer that protects request handlers cannot catch them; without
// this an unrecovered panic in one job would terminate the whole process and
// take down live traffic.
func recoverJob(job string, fn func()) (recovered any) {
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
func guard(job string, fn func()) { _ = recoverJob(job, fn) }

// jobControl carries the runtime kill switch and the health signal for one
// background job. Every field is touched concurrently: the ticker goroutine
// records outcomes while an operator toggles the switch and reads health at
// runtime, so all access goes through atomics.
type jobControl struct {
	disabled    atomic.Bool
	failures    atomic.Int64
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
	LastSuccess time.Time // zero when the job has never succeeded
	LastFailure time.Time // zero when the job has never failed
}

// job returns the control block for name, creating it on first use so a job's
// health is queryable from the moment it is registered.
func (a *App) job(name string) *jobControl {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	if a.jobs == nil {
		a.jobs = make(map[string]*jobControl)
	}
	jc, ok := a.jobs[name]
	if !ok {
		jc = &jobControl{}
		a.jobs[name] = jc
	}
	return jc
}

// SetJobEnabled flips a background job's kill switch at runtime. A disabled job
// stays registered and keeps ticking, but each tick returns early without doing
// work, so an operator can stop a misbehaving job without a redeploy.
func (a *App) SetJobEnabled(name string, enabled bool) {
	a.job(name).disabled.Store(!enabled)
}

// JobHealth returns a snapshot of every registered background job's health,
// ordered by name for stable output.
func (a *App) JobHealth() []JobHealth {
	a.jobsMu.Lock()
	defer a.jobsMu.Unlock()
	out := make([]JobHealth, 0, len(a.jobs))
	for name, jc := range a.jobs {
		out = append(out, JobHealth{
			Name:        name,
			Enabled:     !jc.disabled.Load(),
			Failures:    jc.failures.Load(),
			LastSuccess: nanosToTime(jc.lastSuccess.Load()),
			LastFailure: nanosToTime(jc.lastFailure.Load()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// nanosToTime maps a stored unix-nano timestamp back to a time.Time, keeping the
// "never happened" sentinel (0) as the zero time rather than the unix epoch.
func nanosToTime(nanos int64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos).UTC()
}

func (a *App) whenLeader(name string, start func(context.Context)) {
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

func (a *App) drainBackground(timeout time.Duration) shutdownOutcome {
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return shutdownOutcome{name: "background tasks", completed: true}
	case <-time.After(timeout):
		slog.Warn("background task drain timed out", "timeout", timeout.String())
		return shutdownOutcome{name: "background tasks", completed: false}
	}
}

// drainSearchBackground waits for the discovery search service's own detached
// background work (identity-bridge persistence, telemetry emit, vocab ingest,
// all on context.WithoutCancel) to finish before cleanup() closes the DB pool
// and Redis client out from under it. It is bounded so a wedged task cannot
// stall the shutdown sequence, and reports its outcome like every other
// component.
func (a *App) drainSearchBackground(timeout time.Duration) shutdownOutcome {
	if a.searchSvc == nil {
		return shutdownOutcome{name: "discovery search", completed: true}
	}
	done := make(chan struct{})
	go func() {
		a.searchSvc.WaitForBackground()
		close(done)
	}()
	select {
	case <-done:
		return shutdownOutcome{name: "discovery search", completed: true}
	case <-time.After(timeout):
		slog.Warn("discovery search background drain timed out", "timeout", timeout.String())
		return shutdownOutcome{name: "discovery search", completed: false}
	}
}

func (a *App) startTicker(ctx context.Context, name string, interval time.Duration, fn func() error) {
	a.whenLeader(name, func(ctx context.Context) { a.runTicker(ctx, name, interval, fn) })
}

func (a *App) runTicker(ctx context.Context, name string, interval time.Duration, fn func() error) {
	jc := a.job(name)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.tick(jc, name, fn)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.tick(jc, name, fn)
			}
		}
	}()
}

// tick runs one scheduled invocation, honouring the kill switch, re-checking
// leadership (a blue-green deploy can hand the lock to another instance after a
// job first started, and a job that kept firing would duplicate the new
// leader's work), containing any panic, and recording the outcome as the job's
// health signal. A recovered panic counts as a failed run.
func (a *App) tick(jc *jobControl, name string, fn func() error) {
	if jc.disabled.Load() || !a.stillLeader() {
		return
	}
	var err error
	if r := recoverJob(name, func() { err = fn() }); r != nil {
		err = fmt.Errorf("panic: %v", r)
	}
	jc.record(err)
}

// stillLeader reports whether this instance currently holds leadership. With no
// election configured (unit tests of the ticker mechanics, single-process
// setups) it returns true so the job runs unconditionally, matching the
// pre-election behaviour.
func (a *App) stillLeader() bool {
	return a.election == nil || a.election.IsLeader()
}
