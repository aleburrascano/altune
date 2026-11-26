package background

import (
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const runBudgetFraction = 2

const backgroundJobPanic = "background_job.panic"

var ErrTickerRequired = errors.New("a repeating job needs a ticker")

var ErrRunBudgetExceeded = errors.New("background job run exceeded its budget")

type NamedStart struct {
	Name  string
	Start func(context.Context)
}

func RunBudget(interval time.Duration) time.Duration {
	return interval / runBudgetFraction
}

func BindRun(ctx context.Context, name string) context.Context {
	return logging.ContextWithJobRun(ctx, name, uuid.NewString())
}

func RunJob(parent context.Context, control *JobControl, now clock.Func, name string, budget time.Duration, fn func(context.Context) error) {
	if now == nil {
		panic("background.RunJob: a nil clock")
	}
	jobCtx, cancel := context.WithTimeoutCause(parent, budget, ErrRunBudgetExceeded)
	defer cancel()
	jobCtx = BindRun(jobCtx, name)
	var err error
	if panicked := SafeRun(jobCtx, backgroundJobPanic, func() { err = fn(jobCtx) }, "job", name); panicked != nil {
		err = panicked
	}
	overBudget := err != nil && errors.Is(context.Cause(jobCtx), ErrRunBudgetExceeded)
	if overBudget {
		slog.WarnContext(jobCtx, "background job run exceeded its budget; canceled",
			"job", name, "budget", budget.String(), "error", err)
	}
	if err != nil && !overBudget && parent.Err() != nil {
		slog.InfoContext(jobCtx, "background job run interrupted; not counted as a failure",
			"job", name, "cause", context.Cause(parent))
		return
	}
	control.Record(now(), err)
}

func Every(ctx context.Context, bg *Tracker, interval time.Duration, newTicker clock.TickerFunc, run func()) error {
	if newTicker == nil {
		return fmt.Errorf("background.Every: %w", ErrTickerRequired)
	}
	bg.Track(func() {
		run()
		ticks, stop := newTicker(interval)
		defer stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticks:
				run()
			}
		}
	})
	return nil
}

func StartWhenLeader(ctx context.Context, bg *Tracker, await func(context.Context) bool, starts []NamedStart) {
	bg.Track(func() {
		if !await(ctx) {
			return
		}
		for _, job := range starts {
			_ = SafeRun(ctx, backgroundJobPanic, func() { job.Start(ctx) }, "job", job.Name)
		}
	})
}
