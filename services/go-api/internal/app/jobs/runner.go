package jobs

import (
	"altune/go-api/internal/shared/leader"
	"altune/go-api/internal/shared/runloop"
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func NewElection(pool *pgxpool.Pool) *leader.Election {
	return leader.NewElection(leader.PoolLocks(pool), backgroundLockKey, backgroundLockInterval)
}

type Runner struct {
	wg       sync.WaitGroup
	registry runloop.JobRegistry
	election Election
	starts   []runloop.NamedStart
}

func (r *Runner) Background() *sync.WaitGroup {
	return &r.wg
}

func (r *Runner) Control(name Name) *runloop.JobControl {
	return r.registry.Control(string(name))
}

func (r *Runner) Switch(name Name) func() bool {
	return r.Control(name).Admit
}

func (r *Runner) SetEnabled(name Name, enabled bool) (runloop.JobHealth, bool) {
	return r.registry.SetEnabled(string(name), enabled)
}

func (r *Runner) Health() []runloop.JobHealth {
	return r.registry.Health()
}

func (r *Runner) WhenLeader(name Name, start func(context.Context)) {
	r.starts = append(r.starts, runloop.NamedStart{Name: string(name), Start: start})
}

func (r *Runner) Ticker(name Name, interval time.Duration, fn func(context.Context) error) {
	r.Control(name)
	r.WhenLeader(name, func(ctx context.Context) {
		run := r.newRun(name, interval)
		runloop.Every(ctx, &r.wg, interval, func() { r.tickAsLeader(ctx, run, fn) })
	})
}

func (r *Runner) EveryInstance(ctx context.Context, name Name, interval time.Duration, fn func(context.Context) error) {
	run := r.newRun(name, interval)
	runloop.Every(ctx, &r.wg, interval, func() { r.tick(ctx, run, fn) })
}

func (r *Runner) Lead(ctx context.Context, election Election) {
	r.election = election
	election.Start(ctx)
	runloop.StartWhenLeader(ctx, &r.wg, election.Await, r.starts)
}

func (r *Runner) Election() Election {
	return r.election
}

func (r *Runner) LeaderContext(ctx context.Context) (context.Context, context.CancelFunc, bool) {
	if r.election == nil {
		return runloop.EveryPassLeads(ctx)
	}
	return r.election.LeaderContext(ctx)
}

type jobRun struct {
	control *runloop.JobControl
	name    Name
	budget  time.Duration
}

func (r *Runner) newRun(name Name, interval time.Duration) jobRun {
	return jobRun{control: r.Control(name), name: name, budget: runloop.RunBudget(interval)}
}

func (r *Runner) tick(ctx context.Context, run jobRun, fn func(context.Context) error) {
	if !run.control.Admit() {
		return
	}
	runloop.RunJob(ctx, run.control, string(run.name), run.budget, fn)
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
	runloop.RunJob(leaderCtx, run.control, string(run.name), run.budget, fn)
}
