package runloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

const runBudgetFraction = 2

var ErrRunBudgetExceeded = errors.New("background job run exceeded its budget")

type NamedStart struct {
	Name  string
	Start func(context.Context)
}

func Recover(name string, fn func()) (recovered any) {
	defer func() {
		if r := recover(); r != nil {
			recovered = r
			slog.Error("background job panicked; recovered",
				"job", name, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	fn()
	return nil
}

func RunBudget(interval time.Duration) time.Duration {
	return interval / runBudgetFraction
}

func RunJob(parent context.Context, control *JobControl, name string, budget time.Duration, fn func(context.Context) error) {
	jobCtx, cancel := context.WithTimeoutCause(parent, budget, ErrRunBudgetExceeded)
	defer cancel()
	var err error
	if r := Recover(name, func() { err = fn(jobCtx) }); r != nil {
		err = fmt.Errorf("panic: %v", r)
	}
	overBudget := err != nil && errors.Is(context.Cause(jobCtx), ErrRunBudgetExceeded)
	if overBudget {
		slog.Warn("background job run exceeded its budget; canceled",
			"job", name, "budget", budget.String(), "error", err)
	}
	if err != nil && !overBudget && parent.Err() != nil {
		slog.Info("background job run interrupted; not counted as a failure",
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
			Recover(job.Name, func() { job.Start(ctx) })
		}
	}()
}
