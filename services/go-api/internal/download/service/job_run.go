package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"errors"
)

type jobRun struct {
	attempt           int
	claim             ports.ClaimNumber
	schedulerOwned    bool
	rollbackHandedOff bool
}

type jobRunKey struct{}

func jobRunFrom(ctx context.Context) jobRun {
	if ctx == nil {
		return jobRun{}
	}
	run, _ := ctx.Value(jobRunKey{}).(jobRun)
	return run
}

func withJobRun(ctx context.Context, change func(*jobRun)) context.Context {
	run := jobRunFrom(ctx)
	change(&run)
	return context.WithValue(ctx, jobRunKey{}, run)
}

func withSchedulerOwnedJobContext(ctx context.Context) context.Context {
	return withJobRun(ctx, func(r *jobRun) { r.schedulerOwned = true })
}

func withJobAttempt(ctx context.Context, attempt int) context.Context {
	return withJobRun(ctx, func(r *jobRun) { r.attempt = attempt })
}

func withJobFence(ctx context.Context, fence ports.ClaimNumber) context.Context {
	return withJobRun(ctx, func(r *jobRun) { r.claim = fence })
}

func withRollbackHandedOver(ctx context.Context) context.Context {
	return withJobRun(ctx, func(r *jobRun) { r.rollbackHandedOff = true })
}

func jobAttemptFrom(ctx context.Context) int { return jobRunFrom(ctx).attempt }

func jobFenceFrom(ctx context.Context) ports.ClaimNumber { return jobRunFrom(ctx).claim }

func rollbackHandedOver(ctx context.Context) bool { return jobRunFrom(ctx).rollbackHandedOff }

func schedulerCancelledJobContext(ctx context.Context) bool {
	return jobRunFrom(ctx).schedulerOwned && ctx.Err() != nil
}

func schedulerHandedOver(ctx context.Context) bool {
	return jobRunFrom(ctx).schedulerOwned && errors.Is(ctx.Err(), context.Canceled)
}
