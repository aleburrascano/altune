package runloop

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const runBudgetFraction = 2

const backgroundJobPanic = "background_job.panic"

var ErrRunBudgetExceeded = errors.New("background job run exceeded its budget")

type NamedStart struct {
	Name  string
	Start func(context.Context)
}

func RunBudget(interval time.Duration) time.Duration {
	return interval / runBudgetFraction
}

func RunJob(parent context.Context, control *JobControl, name string, budget time.Duration, fn func(context.Context) error) {
	jobCtx, cancel := context.WithTimeoutCause(parent, budget, ErrRunBudgetExceeded)
	defer cancel()
	var err error
	if panicked := Try(jobCtx, backgroundJobPanic, func() { err = fn(jobCtx) }, "job", name); panicked != nil {
		err = panicked
	}
	overBudget := err != nil && errors.Is(context.Cause(jobCtx), ErrRunBudgetExceeded)
	if overBudget {
		slog.WarnContext(jobCtx, "background job run exceeded its budget; canceled",
			"job", name, "budget", budget.String(), "error", err)
	}
	if err != nil && !overBudget && parent.Err() != nil {
		slog.InfoContext(parent, "background job run interrupted; not counted as a failure",
			"job", name, "cause", context.Cause(parent))
		return
	}
	control.Record(err)
}

func Every(ctx context.Context, wg *sync.WaitGroup, interval time.Duration, run func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
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

func StartWhenLeader(ctx context.Context, wg *sync.WaitGroup, await func(context.Context) bool, starts []NamedStart) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		if !await(ctx) {
			return
		}
		for _, job := range starts {
			_ = Try(ctx, backgroundJobPanic, func() { job.Start(ctx) }, "job", job.Name)
		}
	}()
}
