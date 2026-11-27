package jobrun_test

import (
	"altune/go-api/internal/download/service/jobrun"
	"context"
	"errors"
	"testing"
)

func TestJob_AJobWithoutAReporterReportsToNobody(t *testing.T) {
	var job jobrun.Job

	job.Progress().Meta("title", "artist", "album")
	job.Progress().Stage(context.Background(), "search")
}

func TestJob_VerifyCommitAsksTheCheckItCarries(t *testing.T) {
	refused := errors.New("lease lost")
	cases := []struct {
		name string
		job  jobrun.Job
		want error
	}{
		{"no check admits the commit", jobrun.Job{}, nil},
		{"a refusing check refuses it", jobrun.Job{CommitCheck: func(context.Context) error { return refused }}, refused},
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
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithTimeout(context.Background(), 0)
	defer expire()
	cases := []struct {
		name          string
		job           jobrun.Job
		ctx           context.Context
		wantCancelled bool
		wantHandedOff bool
	}{
		{"owned and cancelled", jobrun.Job{SchedulerOwned: true}, cancelled, true, true},
		{"owned and past its deadline", jobrun.Job{SchedulerOwned: true}, expired, true, false},
		{"owned and still running", jobrun.Job{SchedulerOwned: true}, context.Background(), false, false},
		{"not owned and cancelled", jobrun.Job{}, cancelled, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.job.CancelledByScheduler(tc.ctx); got != tc.wantCancelled {
				t.Errorf("CancelledByScheduler = %v, want %v", got, tc.wantCancelled)
			}
			if got := tc.job.HandedOverByScheduler(tc.ctx); got != tc.wantHandedOff {
				t.Errorf("HandedOverByScheduler = %v, want %v", got, tc.wantHandedOff)
			}
		})
	}
}
