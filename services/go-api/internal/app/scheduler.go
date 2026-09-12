package app

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/shared/leader"
)

const backgroundLockKey int64 = 8_246_113_907_441_002

func (a *App) whenLeader(start func(context.Context)) {
	a.backgroundStarts = append(a.backgroundStarts, start)
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
		for _, start := range a.backgroundStarts {
			start(ctx)
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

func (a *App) startTicker(ctx context.Context, interval time.Duration, fn func()) {
	a.whenLeader(func(ctx context.Context) { a.runTicker(ctx, interval, fn) })
}

func (a *App) runTicker(ctx context.Context, interval time.Duration, fn func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		fn()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
}
