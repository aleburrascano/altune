package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/inmemoryqueue"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"sync"
	"testing"
	"time"
)

type jobOutcome struct {
	released bool
	settled  bool
	delay    time.Duration
}

type firstOutcomeQueue struct {
	*inmemoryqueue.Queue
	run     *int
	once    sync.Once
	outcome chan jobOutcome
}

func (q *firstOutcomeQueue) Claim(ctx context.Context, lease time.Duration) (ports.Job, error) {
	job, err := q.Queue.Claim(ctx, lease)
	if err == nil && q.run != nil {
		job.Run = *q.run
	}
	return job, err
}

func (q *firstOutcomeQueue) Release(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber, delay time.Duration) error {
	q.once.Do(func() { q.outcome <- jobOutcome{released: true, delay: delay} })
	return q.Queue.Release(ctx, trackID, fence, delay)
}

func (q *firstOutcomeQueue) Settle(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber) error {
	q.once.Do(func() { q.outcome <- jobOutcome{settled: true} })
	return q.Queue.Settle(ctx, trackID, fence)
}

type jobRunner interface {
	Execute(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
	ExecuteReplace(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
	RefuseQueued(ctx context.Context, userId ids.UserId, trackId ids.TrackId)
}

func runOneJob(t *testing.T, acquirer jobRunner, track acquisitiontest.LedgerTrack) jobOutcome {
	t.Helper()
	return scheduleOnce(t, acquirer, track, nil)
}

func runOneJobAs(t *testing.T, acquirer jobRunner, track acquisitiontest.LedgerTrack, run int) jobOutcome {
	t.Helper()
	return scheduleOnce(t, acquirer, track, &run)
}

func scheduleOnce(t *testing.T, acquirer jobRunner, track acquisitiontest.LedgerTrack, run *int, opts ...func(*service.BackgroundAcquisitionScheduler)) jobOutcome {
	t.Helper()
	queue := &firstOutcomeQueue{Queue: inmemoryqueue.New(make(chan struct{}, 1), time.Now), run: run, outcome: make(chan jobOutcome, 1)}
	opts = append([]func(*service.BackgroundAcquisitionScheduler){service.WithJobQueue(queue)}, opts...)
	scheduler := service.NewBackgroundAcquisitionScheduler(acquirer, 1, opts...)
	defer scheduler.Shutdown(context.Background())
	if err := scheduler.Schedule(context.Background(), track.UserId, track.ID, ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}
	return <-queue.outcome
}
