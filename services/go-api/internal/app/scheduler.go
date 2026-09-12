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

func (a *App) drainBackground(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("background task drain timed out")
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
