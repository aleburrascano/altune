package app

import (
	"context"
	"log/slog"
	"runtime/debug"
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

// guard runs a scheduled job and recovers from any panic it raises, logging the
// failure (named) and returning so the caller can continue. Scheduled jobs run
// in their own goroutines outside any HTTP request, so the httputil.Recoverer
// that protects request handlers cannot catch them; without this an unrecovered
// panic in one job would terminate the whole process and take down live traffic.
func guard(job string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("background job panicked; recovered",
				"job", job, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	fn()
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

func (a *App) startTicker(ctx context.Context, name string, interval time.Duration, fn func()) {
	a.whenLeader(name, func(ctx context.Context) { a.runTicker(ctx, name, interval, fn) })
}

func (a *App) runTicker(ctx context.Context, name string, interval time.Duration, fn func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		guard(name, fn)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				guard(name, fn)
			}
		}
	}()
}
