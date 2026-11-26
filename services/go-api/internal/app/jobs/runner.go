package jobs

import (
	"altune/go-api/internal/platform/leader"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	backgroundLockKey      int64 = 8_246_113_907_441_002
	backgroundLockInterval       = 10 * time.Second
)

type Election interface {
	Start(context.Context)
	Await(context.Context) bool
	LeaderContext(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)
	Shutdown(context.Context)
}

func NewElection(locks leader.LockStore) (*leader.Election, error) {
	return leader.NewElection(locks, backgroundLockKey, backgroundLockInterval, clock.Now, clock.NewTimer, clock.NewTicker)
}

type Runner struct {
	Now       clock.Func
	NewTicker clock.TickerFunc

	loops    background.Tracker
	registry background.JobRegistry
	election Election
	led      bool
	starts   []background.NamedStart
}

func (r *Runner) Background() *background.Tracker {
	return &r.loops
}

func (r *Runner) Wait() {
	r.loops.Wait()
}

func (r *Runner) Control(name Name) *background.JobControl {
	return r.registry.Control(string(name))
}

func (r *Runner) Switch(name Name) func() bool {
	return r.Control(name).Admit
}

func (r *Runner) SetEnabled(name Name, enabled bool) (background.JobHealth, bool) {
	return r.registry.SetEnabled(string(name), enabled)
}

func (r *Runner) Health() []background.JobHealth {
	return r.registry.Health()
}

func (r *Runner) WhenLeader(name Name, start func(context.Context)) {
	r.starts = append(r.starts, background.NamedStart{Name: string(name), Start: start})
}

func (r *Runner) Ticker(name Name, interval time.Duration, fn func(context.Context) error) {
	if runsOnEveryInstance(name) {
		panic(fmt.Sprintf("jobs: %q runs on every instance, so it registers with EveryInstance, not the leader-only Ticker", name))
	}
	r.requireClock(name)
	r.Control(name)
	r.WhenLeader(name, func(ctx context.Context) {
		run := r.newRun(name, interval)
		if err := background.Every(ctx, &r.loops, interval, r.NewTicker, func() { r.tickAsLeader(ctx, run, fn) }); err != nil {
			slog.ErrorContext(ctx, "job not started", "job", string(name), "error", err)
		}
	})
}

func (r *Runner) EveryInstance(ctx context.Context, name Name, interval time.Duration, fn func(context.Context) error) {
	if !runsOnEveryInstance(name) {
		panic(fmt.Sprintf("jobs: %q is leader-only, so it registers with Ticker; only a job the table marks everyInstance may use EveryInstance", name))
	}
	r.requireClock(name)
	run := r.newRun(name, interval)
	if err := background.Every(ctx, &r.loops, interval, r.NewTicker, func() { r.tick(ctx, run, fn) }); err != nil {
		panic(fmt.Sprintf("jobs: %q: %v", name, err))
	}
}

func (r *Runner) requireClock(name Name) {
	if r.Now == nil || r.NewTicker == nil {
		panic(fmt.Sprintf("jobs: %q needs the Runner's Now and NewTicker set before it registers", name))
	}
}

func (r *Runner) Lead(ctx context.Context, election Election) {
	r.election = election
	r.led = true
	election.Start(ctx)
	background.StartWhenLeader(ctx, &r.loops, election.Await, r.starts)
}

func (r *Runner) Election() Election {
	return r.election
}

func (r *Runner) LeaderCounters() leader.Counters {
	if e, ok := r.election.(interface{ Counters() leader.Counters }); ok {
		return e.Counters()
	}
	return leader.Counters{}
}

func (r *Runner) LeaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if !r.led {
		return background.EveryPassLeads(ctx)
	}
	return r.election.LeaderContext(ctx)
}

type jobRun struct {
	control *background.JobControl
	name    Name
	budget  time.Duration
}

func (r *Runner) newRun(name Name, interval time.Duration) jobRun {
	return jobRun{control: r.Control(name), name: name, budget: background.RunBudget(interval)}
}

func (r *Runner) tick(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	background.RunJob(ctx, run.control, r.Now, string(run.name), run.budget, fn)
}

func (r *Runner) tickAsLeader(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	leaderCtx, release, ok := r.LeaderContext(ctx)
	if !ok {
		return
	}
	defer release()
	background.RunJob(leaderCtx, run.control, r.Now, string(run.name), run.budget, fn)
}

func (r *Runner) StartTicker(ctx context.Context, name Name, interval time.Duration, run func(context.Context) error, startedAttrs ...any) {
	r.Ticker(name, interval, run)
	slog.InfoContext(ctx, string(name)+" started", startedAttrs...)
}

func WarnOnFailure(name Name, run func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := run(ctx); err != nil {
			slog.WarnContext(ctx, string(name)+" failed", "error", err)
			return err
		}
		return nil
	}
}
