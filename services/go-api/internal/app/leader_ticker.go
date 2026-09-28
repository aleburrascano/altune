package app

import (
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/runloop"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

const backgroundLockKey int64 = 8_246_113_907_441_002

type backgroundJob struct {
	name  jobName
	start func(context.Context)
}

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

func guard(job jobName, fn func()) { _ = recoverJob(job, fn) }

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

func (a *App) startTicker(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	a.job(name)
	a.whenLeader(name, func(ctx context.Context) { a.runTicker(ctx, name, interval, fn) })
}

const jobRunBudgetFraction = 2

var errJobRunBudgetExceeded = errors.New("background job run exceeded its budget")

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

func (a *App) leaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if a.election == nil {
		return runloop.EveryPassLeads(ctx)
	}
	return a.election.LeaderContext(ctx)
}
