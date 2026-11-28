package domain

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"time"
)

type JobReporter interface {
	Meta(title, artist, album string)
	Stage(ctx context.Context, name string)
	Source(url string)
	Provenance(value string)
	Timing(stage string, d time.Duration)
	Fetched(format SourceFormat)
	Stored(file StoredFile)
	Decided(confidence float64, sourceKey string, target, measured float64)
}

type AcquisitionJob struct {
	UserID  ids.UserId
	TrackID ids.TrackId

	Attempt        int
	Claim          ClaimNumber
	SchedulerOwned bool

	Reporter    JobReporter
	CommitCheck func(ctx context.Context) error
}

type quietReporter struct{}

func (quietReporter) Meta(_, _, _ string)                       {}
func (quietReporter) Stage(_ context.Context, _ string)         {}
func (quietReporter) Source(_ string)                           {}
func (quietReporter) Provenance(_ string)                       {}
func (quietReporter) Timing(_ string, _ time.Duration)          {}
func (quietReporter) Fetched(_ SourceFormat)                    {}
func (quietReporter) Stored(_ StoredFile)                       {}
func (quietReporter) Decided(_ float64, _ string, _, _ float64) {}

func (j AcquisitionJob) Progress() JobReporter {
	if j.Reporter == nil {
		return quietReporter{}
	}
	return j.Reporter
}

func (j AcquisitionJob) VerifyCommit(ctx context.Context) error {
	if j.CommitCheck == nil {
		return nil
	}
	return j.CommitCheck(ctx)
}

func (j AcquisitionJob) CancelledByScheduler(ctx context.Context) bool {
	return j.SchedulerOwned && ctx.Err() != nil
}

func (j AcquisitionJob) HandedOverByScheduler(ctx context.Context) bool {
	return j.SchedulerOwned && errors.Is(ctx.Err(), context.Canceled)
}
