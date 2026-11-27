package jobrun

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"time"
)

type Reporter interface {
	Meta(title, artist, album string)
	Stage(ctx context.Context, name string)
	Source(url string)
	Provenance(value string)
	Timing(stage string, d time.Duration)
	Fetched(format ports.SourceFormat)
	Stored(file ports.StoredFile)
	Decided(confidence float64, sourceKey string, target, measured float64)
}

type Job struct {
	UserID  ids.UserId
	TrackID ids.TrackId

	Attempt        int
	Claim          ports.ClaimNumber
	SchedulerOwned bool

	Reporter    Reporter
	CommitCheck func(ctx context.Context) error
}

type quiet struct{}

func (quiet) Meta(_, _, _ string)                       {}
func (quiet) Stage(_ context.Context, _ string)         {}
func (quiet) Source(_ string)                           {}
func (quiet) Provenance(_ string)                       {}
func (quiet) Timing(_ string, _ time.Duration)          {}
func (quiet) Fetched(_ ports.SourceFormat)              {}
func (quiet) Stored(_ ports.StoredFile)                 {}
func (quiet) Decided(_ float64, _ string, _, _ float64) {}

func (j Job) Progress() Reporter {
	if j.Reporter == nil {
		return quiet{}
	}
	return j.Reporter
}

func (j Job) VerifyCommit(ctx context.Context) error {
	if j.CommitCheck == nil {
		return nil
	}
	return j.CommitCheck(ctx)
}

func (j Job) CancelledByScheduler(ctx context.Context) bool {
	return j.SchedulerOwned && ctx.Err() != nil
}

func (j Job) HandedOverByScheduler(ctx context.Context) bool {
	return j.SchedulerOwned && errors.Is(ctx.Err(), context.Canceled)
}
