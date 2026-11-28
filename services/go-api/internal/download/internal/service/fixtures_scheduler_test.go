package service_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/random/randomtest"
	"context"
	"fmt"
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
	*memory.Queue
	run     *int
	once    sync.Once
	outcome chan jobOutcome
}

func (q *firstOutcomeQueue) Claim(ctx context.Context, lease time.Duration) (domain.Job, error) {
	job, err := q.Queue.Claim(ctx, lease)
	if err == nil && q.run != nil {
		job.Run = *q.run
	}
	return job, err
}

func (q *firstOutcomeQueue) Release(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber, delay time.Duration) error {
	q.once.Do(func() { q.outcome <- jobOutcome{released: true, delay: delay} })
	return q.Queue.Release(ctx, trackID, fence, delay)
}

func (q *firstOutcomeQueue) Settle(ctx context.Context, trackID ids.TrackId, fence domain.ClaimNumber) error {
	q.once.Do(func() { q.outcome <- jobOutcome{settled: true} })
	return q.Queue.Settle(ctx, trackID, fence)
}

type jobRunner interface {
	Execute(ctx context.Context, job domain.AcquisitionJob) error
	ExecuteReplace(ctx context.Context, job domain.AcquisitionJob) error
	RefuseQueued(ctx context.Context, job domain.AcquisitionJob)
}

func runOneJob(t *testing.T, acquirer jobRunner, track memory.LedgerTrack) jobOutcome {
	t.Helper()
	return scheduleOnce(t, acquirer, track, nil)
}

func runOneJobAs(t *testing.T, acquirer jobRunner, track memory.LedgerTrack, run int) jobOutcome {
	t.Helper()
	return scheduleOnce(t, acquirer, track, &run)
}

func scheduleOnce(t *testing.T, acquirer jobRunner, track memory.LedgerTrack, run *int, opts ...any) jobOutcome {
	t.Helper()
	queue := &firstOutcomeQueue{Queue: memory.New(make(chan struct{}, 1), time.Now), run: run, outcome: make(chan jobOutcome, 1)}
	opts = append([]any{withTestQueue(queue)}, opts...)
	scheduler := newTestScheduler(acquirer, 1, time.Now, opts...)
	defer scheduler.Shutdown(context.Background())
	if err := scheduler.Schedule(context.Background(), track.UserId, track.ID, ""); err != nil {
		t.Fatalf("Schedule = %v, want nil", err)
	}
	return <-queue.outcome
}

type schedulerPortsOption func(*service.SchedulerPorts)

func testSchedulerPorts() service.SchedulerPorts {
	return service.SchedulerPorts{
		Queue:               memory.NewQueueFactory(),
		Upgrades:            service.NoUpgradeLane(),
		Notifier:            memory.NewDatabase(clock.Now).JobNotifier(clock.Sleep),
		Outcomes:            memory.NewDatabase(clock.Now).Outcomes(),
		Events:              events.NoopPublisher(),
		VerifySkips:         func() uint64 { return 0 },
		FingerprintVerified: func() bool { return false },
		Timer:               clock.NewTimer,
		Ticker:              clock.NewTicker,
		Unit:                randomtest.Fixed(0.5),
		Verification:        domain.Verification{Ffprobe: true, Ffmpeg: true, Fpcalc: true, YtDlp: true},
	}
}

func newTestScheduler(acquirer jobRunner, workers int, now func() time.Time, opts ...any) *service.BackgroundAcquisitionScheduler {
	wiring := testSchedulerPorts()
	var public []func(*service.BackgroundAcquisitionScheduler)
	for _, opt := range opts {
		switch opt := opt.(type) {
		case schedulerPortsOption:
			opt(&wiring)
		case func(*service.BackgroundAcquisitionScheduler):
			public = append(public, opt)
		default:
			panic(fmt.Sprintf("newTestScheduler: unsupported option %T", opt))
		}
	}
	return service.MustNewBackgroundAcquisitionScheduler(context.Background(), acquirer, workers, now, wiring, public...)
}

func withTestOutcomes(r ports.OutcomeRecorder) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Outcomes = r }
}

func withTestToolsPresent(v domain.Verification) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Verification = v }
}

func withTestUnit(unit random.Unit) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Unit = unit }
}

func withTestSchedulerEvents(pub events.Publisher) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Events = pub }
}

func withTestNotifier(n ports.JobNotifier) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Notifier = n }
}

func withTestVerification(skips func() uint64, fingerprinted func() bool) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) {
		wiring.VerifySkips = skips
		wiring.FingerprintVerified = fingerprinted
	}
}

func withTestQueue(q ports.DownloadQueue) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Queue = service.QueueOf(q) }
}

func withTestUpgrades(runner upgradeRunner, workers int) schedulerPortsOption {
	return func(wiring *service.SchedulerPorts) { wiring.Upgrades = service.MustUpgradeLaneOf(runner, workers) }
}

type upgradeRunner interface {
	ExecuteUpgrade(ctx context.Context, job domain.AcquisitionJob) error
}
