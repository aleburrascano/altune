package app

import (
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/runloop"
	"context"
	"time"
)

const backgroundLockKey int64 = 8_246_113_907_441_002

type backgroundJob struct {
	name  jobName
	start func(context.Context)
}

func (a *App) whenLeader(name jobName, start func(context.Context)) {
	a.backgroundStarts = append(a.backgroundStarts, backgroundJob{name: name, start: start})
}

func (a *App) whenLeaderUnlessDisabled(name jobName, start func(context.Context)) {
	jc := a.job(name)
	a.whenLeader(name, func(ctx context.Context) {
		if !jc.Admit() {
			return
		}
		start(ctx)
	})
}

func (a *App) startBackgroundWhenLeader(ctx context.Context) {
	a.election = leader.NewElection(a.pool, backgroundLockKey)
	a.election.Start(ctx)
	starts := make([]runloop.NamedStart, len(a.backgroundStarts))
	for i, job := range a.backgroundStarts {
		starts[i] = runloop.NamedStart{Name: string(job.name), Start: job.start}
	}
	runloop.StartWhenLeader(ctx, &a.wg, a.election.Await, starts)
}

func (a *App) startTicker(name jobName, interval time.Duration, fn func(context.Context) error) {
	a.job(name)
	a.whenLeader(name, func(ctx context.Context) { a.runTicker(ctx, name, interval, fn) })
}

type jobRun struct {
	control *runloop.JobControl
	name    jobName
	budget  time.Duration
}

func (a *App) jobRun(name jobName, interval time.Duration) jobRun {
	return jobRun{control: a.job(name), name: name, budget: runloop.RunBudget(interval)}
}

func (a *App) runTicker(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	run := a.jobRun(name, interval)
	runloop.Every(ctx, &a.wg, interval, func() { a.tick(ctx, run, fn) })
}

func (a *App) startEveryInstanceTicker(ctx context.Context, name jobName, interval time.Duration, fn func(context.Context) error) {
	run := a.jobRun(name, interval)
	runloop.Every(ctx, &a.wg, interval, func() { a.tickEveryInstance(ctx, run, fn) })
}

func (a *App) tickEveryInstance(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	runloop.RunJob(ctx, run.control, string(run.name), run.budget, fn)
}

func (a *App) tick(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	leaderCtx, release, ok := a.leaderContext(ctx)
	if !ok {
		return
	}
	defer release()
	runloop.RunJob(leaderCtx, run.control, string(run.name), run.budget, fn)
}

func (a *App) leaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if a.election == nil {
		return runloop.EveryPassLeads(ctx)
	}
	return a.election.LeaderContext(ctx)
}
