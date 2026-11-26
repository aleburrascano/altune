package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"errors"
)

type JobRun struct {
	Attempt           int
	Claim             ports.ClaimNumber
	SchedulerOwned    bool
	rollbackHandedOff bool
}

type jobRunKey struct{}

func jobRunFrom(ctx context.Context) JobRun {
	if ctx == nil {
		return JobRun{}
	}
	run, _ := ctx.Value(jobRunKey{}).(JobRun)
	return run
}

func ContextWithJobRun(ctx context.Context, run JobRun) context.Context {
	run.rollbackHandedOff = jobRunFrom(ctx).rollbackHandedOff
	return context.WithValue(ctx, jobRunKey{}, run)
}

func withJobRun(ctx context.Context, change func(*JobRun)) context.Context {
	run := jobRunFrom(ctx)
	change(&run)
	return context.WithValue(ctx, jobRunKey{}, run)
}

func withSchedulerOwnedJobContext(ctx context.Context) context.Context {
	return withJobRun(ctx, func(r *JobRun) { r.SchedulerOwned = true })
}

func withJobAttempt(ctx context.Context, attempt int) context.Context {
	return withJobRun(ctx, func(r *JobRun) { r.Attempt = attempt })
}

func withJobFence(ctx context.Context, fence ports.ClaimNumber) context.Context {
	return withJobRun(ctx, func(r *JobRun) { r.Claim = fence })
}

func withRollbackHandedOver(ctx context.Context) context.Context {
	return withJobRun(ctx, func(r *JobRun) { r.rollbackHandedOff = true })
}

func jobAttemptFrom(ctx context.Context) int { return jobRunFrom(ctx).Attempt }

func jobFenceFrom(ctx context.Context) ports.ClaimNumber { return jobRunFrom(ctx).Claim }

func rollbackHandedOver(ctx context.Context) bool { return jobRunFrom(ctx).rollbackHandedOff }

func schedulerCancelledJobContext(ctx context.Context) bool {
	return jobRunFrom(ctx).SchedulerOwned && ctx.Err() != nil
}

func schedulerHandedOver(ctx context.Context) bool {
	return jobRunFrom(ctx).SchedulerOwned && errors.Is(ctx.Err(), context.Canceled)
}
