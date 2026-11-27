package ports_test

import (
	"altune/go-api/internal/download/internal/core/ports"
	"context"
	"errors"
	"testing"
)

func runningContext(t *testing.T) context.Context { return t.Context() }

func cancelledContext(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func expiredContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 0)
	t.Cleanup(cancel)
	return ctx
}

func TestJob_AJobWithoutAReporterReportsToNobody(t *testing.T) {
	var job ports.AcquisitionJob

	job.Progress().Meta("title", "artist", "album")
	job.Progress().Stage(context.Background(), "search")
}

func TestJob_VerifyCommitAsksTheCheckItCarries(t *testing.T) {
	refused := errors.New("lease lost")
	cases := []struct {
		name string
		job  ports.AcquisitionJob
		want error
	}{
		{"no check admits the commit", ports.AcquisitionJob{}, nil},
		{"a refusing check refuses it", ports.AcquisitionJob{CommitCheck: func(context.Context) error { return refused }}, refused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.job.VerifyCommit(context.Background()); !errors.Is(got, tc.want) {
				t.Errorf("VerifyCommit = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJob_OnlyASchedulerOwnedJobTreatsItsContextEndingAsTheSchedulersDoing(t *testing.T) {
	cases := []struct {
		name          string
		job           ports.AcquisitionJob
		ctx           func(*testing.T) context.Context
		wantCancelled bool
		wantHandedOff bool
	}{
		{"owned and cancelled", ports.AcquisitionJob{SchedulerOwned: true}, cancelledContext, true, true},
		{"owned and past its deadline", ports.AcquisitionJob{SchedulerOwned: true}, expiredContext, true, false},
		{"owned and still running", ports.AcquisitionJob{SchedulerOwned: true}, runningContext, false, false},
		{"not owned and cancelled", ports.AcquisitionJob{}, cancelledContext, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx(t)
			if got := tc.job.CancelledByScheduler(ctx); got != tc.wantCancelled {
				t.Errorf("CancelledByScheduler = %v, want %v", got, tc.wantCancelled)
			}
			if got := tc.job.HandedOverByScheduler(ctx); got != tc.wantHandedOff {
				t.Errorf("HandedOverByScheduler = %v, want %v", got, tc.wantHandedOff)
			}
		})
	}
}
